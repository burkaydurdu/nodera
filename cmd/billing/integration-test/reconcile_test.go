package integrationtest

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/piggystats/nodera/contexts/billing/async/constants"
	"github.com/piggystats/nodera/contexts/billing/providers"
	"github.com/piggystats/nodera/contexts/billing/providers/fake"
)

// Craftgate does not dedupe by idempotency key: a charge it captured whose
// answer was lost is looked up and adopted, never charged again. An attempt
// younger than twice the call timeout is left alone: its call may still be
// in flight.
func (s *billingSuite) TestReconcile_ACaptureWhoseAnswerWasLostIsAdoptedNotChargedAgain() {
	owner, teamID := s.germanTeam()
	s.droplet(teamID, "web-1", "1.0000", oct(5, 0, 0), at(oct(5, 10, 0)))
	s.fake.NextChargeCapturesThenTimesOut()
	s.closePeriods(closingTime)
	s.Equal("issued", s.onlyInvoice(teamID, owner.Token).str("status"))
	s.Equal("pending", s.attempts(teamID, owner.Token)[0]["status"])
	s.Empty(s.outboxPayloads(constants.PaymentFailed), "a lost answer is no decline")

	s.reconcile(closingTime.Add(30 * time.Second))
	s.Equal("pending", s.attempts(teamID, owner.Token)[0]["status"], "too young to be lost")

	s.reconcile(closingTime.Add(2 * time.Minute))
	s.reconcile(closingTime.Add(10 * time.Minute))

	s.Equal("paid", s.onlyInvoice(teamID, owner.Token).str("status"))
	s.Len(s.fake.Charges, 1, "never charged again")
	s.Equal(1, s.fake.Captures(), "exactly one capture")
	s.Len(s.attempts(teamID, owner.Token), 1)
	s.Len(s.outboxPayloads(constants.PaymentSucceeded), 1)
}

// A provider that refused the call without deciding leaves the attempt open:
// no failure email, and the attempt is charged once the provider holds
// nothing for it.
func (s *billingSuite) TestReconcile_AnUnavailableProviderLeavesTheAttemptOpenThenChargesItOnce() {
	owner, teamID := s.germanTeam()
	s.droplet(teamID, "web-1", "1.0000", oct(5, 0, 0), at(oct(5, 10, 0)))
	s.fake.FailNextCharge(errors.New("payment provider create payment: craftgate SYSTEM_ERROR"))
	s.closePeriods(closingTime)
	inv := s.onlyInvoice(teamID, owner.Token)
	s.Equal("issued", inv.str("status"))
	s.Equal("pending", s.objectList(inv.Body["payments"])[0]["status"])
	s.Empty(s.outboxPayloads(constants.PaymentFailed), "an unavailable provider is no decline")
	s.Empty(s.fake.Charges, "the provider never saw it")

	s.reconcile(closingTime.Add(2 * time.Minute))
	s.reconcile(closingTime.Add(5 * time.Minute))

	s.Require().Len(s.fake.Charges, 1)
	s.Equal("invoice:"+inv.str("id")+":attempt:1", s.fake.Charges[0].IdempotencyKey, "the same attempt, the same key")
	s.Equal(1, s.fake.Captures())
	s.Equal("paid", s.onlyInvoice(teamID, owner.Token).str("status"))
	s.Len(s.outboxPayloads(constants.PaymentSucceeded), 1)
}

// A webhook is a hint: reconciliation trusts the provider's API, settles a
// pending Payment once, and ignores replays and later events.
func (s *billingSuite) TestReconcile_WebhookSettlesAPendingPaymentExactlyOnce() {
	owner, teamID := s.germanTeam()
	s.droplet(teamID, "web-1", "1.0000", oct(5, 0, 0), at(oct(5, 10, 0)))
	s.fake.NextChargePending()
	s.closePeriods(closingTime)
	s.Equal("pending", s.attempts(teamID, owner.Token)[0]["status"])
	var ref string
	s.Require().NoError(s.pgPool.QueryRow(context.Background(), `SELECT provider_ref FROM payments`).Scan(&ref))
	s.Require().NotEmpty(ref)

	headers, body := fake.Webhook(fake.WebhookBody{EventID: "evt-forged", Kind: "payment", PaymentRef: ref, Status: "succeeded"})
	s.Require().Equal(http.StatusOK, s.requestWith(http.MethodPost, craftgateWebhook, string(body), "", headers).Code)
	s.reconcile(closingTime)
	s.Equal("issued", s.onlyInvoice(teamID, owner.Token).str("status"), "the body's claim is not trusted")
	s.Empty(s.outboxPayloads(constants.PaymentSucceeded))

	s.fake.SetPaymentState(ref, providers.StateSucceeded)
	headers, body = fake.Webhook(fake.WebhookBody{EventID: "evt-real", Kind: "payment", PaymentRef: ref, Status: "succeeded"})
	s.Require().Equal(http.StatusOK, s.requestWith(http.MethodPost, craftgateWebhook, string(body), "", headers).Code)
	s.reconcile(closingTime)
	s.Require().Equal(http.StatusOK, s.requestWith(http.MethodPost, craftgateWebhook, string(body), "", headers).Code)
	s.reconcile(closingTime.Add(5 * time.Minute))

	s.Equal("paid", s.onlyInvoice(teamID, owner.Token).str("status"))
	s.Len(s.outboxPayloads(constants.PaymentSucceeded), 1)
	pending, err := s.services.Payments.FindUnprocessedWebhookEvents(context.Background(), 10)
	s.Require().NoError(err)
	s.Empty(pending)
}

// A webhook can name a payment whose reference Nodera never learned (the
// charge's answer was lost): the provider's record names the attempt by its
// key, and the attempt is settled from it.
func (s *billingSuite) TestReconcile_AWebhookForAnUnknownPaymentIsTracedToItsAttempt() {
	owner, teamID := s.germanTeam()
	s.droplet(teamID, "web-1", "1.0000", oct(5, 0, 0), at(oct(5, 10, 0)))
	s.fake.NextChargeCapturesThenTimesOut()
	s.closePeriods(closingTime)
	inv := s.onlyInvoice(teamID, owner.Token)
	captured, err := s.fake.FindCharge(context.Background(), "invoice:"+inv.str("id")+":attempt:1")
	s.Require().NoError(err)

	headers, body := fake.Webhook(fake.WebhookBody{EventID: "evt-lost", Kind: "payment", PaymentRef: captured.ProviderPaymentRef, Status: "succeeded"})
	s.Require().Equal(http.StatusOK, s.requestWith(http.MethodPost, craftgateWebhook, string(body), "", headers).Code)
	s.reconcile(closingTime)

	s.Equal("paid", s.onlyInvoice(teamID, owner.Token).str("status"), "settled through the webhook, not the pending scan")
	s.Len(s.fake.Charges, 1)
	s.Len(s.outboxPayloads(constants.PaymentSucceeded), 1)
}
