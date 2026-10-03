package integrationtest

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/billing/aggregates/invoice"
	"github.com/piggystats/nodera/contexts/billing/aggregates/payment"
	"github.com/piggystats/nodera/contexts/billing/aggregates/paymentmethod"
	"github.com/piggystats/nodera/contexts/billing/async/constants"
	"github.com/piggystats/nodera/contexts/billing/providers"
)

// The receipt says what was paid by which Team and names the profile's invoice
// address; who among the members reads it is the notification context's
// decision (docs/adr/0008), so no recipient list travels.
func (s *billingSuite) TestCharge_SucceededPaysTheInvoiceAndSendsTheReceipt() {
	owner, teamID := s.germanTeam()
	s.droplet(teamID, "web-1", "1.0000", oct(5, 0, 0), at(oct(5, 10, 0)))

	s.closePeriods(closingTime)

	detail := s.onlyInvoice(teamID, owner.Token)
	s.Equal("paid", detail.str("status"))
	receipts := s.outboxPayloads(constants.PaymentSucceeded)
	s.Require().Len(receipts, 1)
	receipt := receipts[0]
	s.Equal(detail.str("number"), receipt["invoice_number"])
	s.Equal(teamID.String(), receipt["team_id"])
	s.Equal(detail.str("id"), receipt["invoice_id"])
	s.NotEmpty(receipt["payment_id"])
	s.Equal("10.00", receipt["amount"])
	s.Equal("USD", receipt["currency"])
	s.Equal(rfc3339(closingTime), receipt["paid_at"])
	s.Equal(map[string]any{"email": "books@acme.example", "full_name": "Acme GmbH", "locale": "en"}, receipt["invoice_address"])
	s.NotContains(receipt, "recipients")
	s.NotContains(receipt, "team_name")

	status := s.get(billingPath(teamID, "/status"), owner.Token)
	s.Equal("active", status.str("status"))
	s.Nil(status.Body["open_invoice_id"])
}

// The full schedule from the first failure F: retries at F+1d, F+3d and
// F+7d with an email each, suspension on the day-7 failure, warnings at
// F+14d and F+20d, the deletion request at F+21d, and nothing after.
func (s *billingSuite) TestDunning_RunsTheScheduleFromRetriesToTheDeletionRequest() {
	owner, teamID := s.germanTeam()
	card := s.get(billingPath(teamID, "/payment-methods"), owner.Token).List[0]
	s.droplet(teamID, "web-1", "1.0000", oct(5, 0, 0), at(oct(5, 10, 0)))
	for range 4 {
		s.fake.NextChargeFails(providers.ReasonInsufficientFunds)
	}
	failedAt := closingTime
	day := func(n int) time.Time { return failedAt.AddDate(0, 0, n) }

	s.closePeriods(failedAt)

	detail := s.onlyInvoice(teamID, owner.Token)
	invoiceID := detail.str("id")
	s.Equal("payment_failed", detail.str("status"))
	failures := s.outboxPayloads(constants.PaymentFailed)
	s.Require().Len(failures, 1)
	s.Equal(float64(1), failures[0]["attempt"])
	s.Equal("insufficient_funds", failures[0]["failure_reason"])
	s.Equal("10.00", failures[0]["amount"])
	s.Equal(rfc3339(day(1)), failures[0]["next_retry_at"])
	status := s.get(billingPath(teamID, "/status"), owner.Token)
	s.Equal("active", status.str("status"))
	s.Equal(invoiceID, status.str("open_invoice_id"))
	s.Equal(rfc3339(day(1)), status.str("next_retry_at"))
	s.Nil(status.Body["suspended_at"])

	s.assertError(s.request(http.MethodDelete, billingPath(teamID, "/payment-methods/"+card["id"].(string)), "", owner.Token),
		http.StatusConflict, int(paymentmethod.Domain), paymentmethod.ErrorLastCardOwed.Code)

	s.runDunning(day(1).Add(-time.Minute))
	s.Len(s.fake.Charges, 1, "nothing is due before day 1")

	s.runDunning(day(1))
	s.runDunning(day(1).Add(time.Hour))
	failures = s.outboxPayloads(constants.PaymentFailed)
	s.Require().Len(failures, 2, "the day-1 retry runs once")
	s.Equal(float64(2), failures[1]["attempt"])
	s.Equal(rfc3339(day(3)), failures[1]["next_retry_at"])

	s.runDunning(day(3))
	failures = s.outboxPayloads(constants.PaymentFailed)
	s.Require().Len(failures, 3)
	s.Equal(rfc3339(day(7)), failures[2]["next_retry_at"])
	s.Empty(s.statusEvents(teamID)[1:], "not suspended before day 7")

	s.runDunning(day(7))
	failures = s.outboxPayloads(constants.PaymentFailed)
	s.Require().Len(failures, 4)
	s.Equal(float64(4), failures[3]["attempt"])
	s.Nil(failures[3]["next_retry_at"], "no retry after day 7")
	s.Equal([]string{"active", "suspended"}, s.statusEvents(teamID))
	suspended := s.outboxPayloads(constants.TeamSuspended)
	s.Require().Len(suspended, 1)
	s.Equal(detail.str("number"), suspended[0]["invoice_number"])
	s.Equal(frontURL+"/dashboard/team/billing?team="+teamID.String()+"&tab=invoices&invoice="+invoiceID, suspended[0]["pay_url"])
	s.Equal(teamID.String(), suspended[0]["team_id"])
	s.Equal(invoiceID, suspended[0]["invoice_id"])
	status = s.get(billingPath(teamID, "/status"), owner.Token)
	s.Equal("suspended", status.str("status"))
	s.Equal(rfc3339(day(7)), status.str("suspended_at"))
	s.Equal(rfc3339(day(21)), status.str("deletion_at"))
	s.Equal(invoiceID, status.str("open_invoice_id"))
	s.Nil(status.Body["next_retry_at"])

	s.runDunning(day(10))
	s.Empty(s.outboxPayloads(constants.DropletsDeletionWarning))

	s.runDunning(day(14))
	warnings := s.outboxPayloads(constants.DropletsDeletionWarning)
	s.Require().Len(warnings, 1)
	s.Equal(rfc3339(day(21)), warnings[0]["deletion_at"])
	s.Equal(float64(14), warnings[0]["dunning_day"], "the day tells the two warnings apart")
	s.Equal(frontURL+"/dashboard/team/billing?team="+teamID.String()+"&tab=invoices&invoice="+invoiceID, warnings[0]["pay_url"])

	s.runDunning(day(20))
	s.Require().Len(s.outboxPayloads(constants.DropletsDeletionWarning), 2)
	s.Equal(float64(20), s.outboxPayloads(constants.DropletsDeletionWarning)[1]["dunning_day"])
	s.Empty(s.outboxPayloads(constants.TeamDropletsDeletionRequested))

	s.runDunning(day(21))
	deletions := s.outboxPayloads(constants.TeamDropletsDeletionRequested)
	s.Require().Len(deletions, 1)
	s.Equal(teamID.String(), deletions[0]["team_id"])
	s.Equal(rfc3339(day(21)), deletions[0]["requested_at"])

	s.runDunning(day(30))
	s.Len(s.outboxPayloads(constants.TeamDropletsDeletionRequested), 1)
	s.Len(s.outboxPayloads(constants.DropletsDeletionWarning), 2)
	s.Len(s.outboxPayloads(constants.PaymentFailed), 4)
	s.Len(s.fake.Charges, 4, "one charge on issue, three retries")
	s.Len(s.objectList(s.invoiceDetail(teamID, owner.Token, invoiceID).Body["payments"]), 4)
}

// A worker that missed days catches up with the latest retry only; paying
// with another card then lifts the suspension.
func (s *billingSuite) TestDunning_PayNowWithAnotherCardLiftsTheSuspension() {
	owner, teamID := s.germanTeam()
	s.fake.NextCard("mastercard", "5555", 9, 2031)
	other := s.addCard(teamID, owner.Token)
	s.droplet(teamID, "web-1", "1.0000", oct(5, 0, 0), at(oct(5, 10, 0)))
	s.fake.NextChargeFails(providers.ReasonDeclined)
	s.fake.NextChargeFails(providers.ReasonDeclined)
	s.closePeriods(closingTime)

	s.runDunning(closingTime.AddDate(0, 0, 8))

	s.Len(s.outboxPayloads(constants.PaymentFailed), 2, "days 1 and 3 were missed; the day-7 retry ran once")
	s.Equal([]string{"active", "suspended"}, s.statusEvents(teamID))
	invoiceID := s.invoices(teamID, owner.Token)[0]["id"].(string)

	paid := s.pay(teamID, owner.Token, invoiceID, fmt.Sprintf(`{"payment_method_id":%q}`, other.str("id")))

	s.Require().Equal(http.StatusAccepted, paid.Code, paid.Raw)
	s.Equal("succeeded", paid.str("status"))
	s.Equal(invoiceID, paid.str("invoice_id"))
	last := s.fake.Charges[len(s.fake.Charges)-1]
	var otherCardRef string
	s.Require().NoError(s.pgPool.QueryRow(context.Background(), `SELECT provider_card_ref FROM payment_methods WHERE id = $1`, other.str("id")).Scan(&otherCardRef))
	s.Equal(otherCardRef, last.Card.CardRef, "charged the card asked for, not the default")
	s.Equal("invoice:"+invoiceID+":attempt:3", last.IdempotencyKey)
	s.Equal("paid", s.invoiceDetail(teamID, owner.Token, invoiceID).str("status"))
	s.Equal([]string{"active", "suspended", "active"}, s.statusEvents(teamID))
	s.statusTimesIncrease(teamID)
	s.Len(s.outboxPayloads(constants.PaymentSucceeded), 1)
	status := s.get(billingPath(teamID, "/status"), owner.Token)
	s.Equal("active", status.str("status"))
	for _, field := range []string{"open_invoice_id", "next_retry_at", "suspended_at", "deletion_at"} {
		s.Nil(status.Body[field], field)
	}

	s.runDunning(closingTime.AddDate(0, 0, 21))
	s.Empty(s.outboxPayloads(constants.TeamDropletsDeletionRequested), "a paid Invoice leaves Dunning")
}

func (s *billingSuite) TestPay_RefusesAPaidInvoiceAndLimitsAttempts() {
	paidOwner, paidTeam := s.germanTeam()
	s.droplet(paidTeam, "web-2", "1.0000", time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC), at(time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)))
	s.closePeriods(october.Add(5 * time.Minute))
	paidID := s.invoices(paidTeam, paidOwner.Token)[0]["id"].(string)
	owner, teamID := s.germanTeam()
	s.droplet(teamID, "web-1", "1.0000", oct(5, 0, 0), at(oct(5, 10, 0)))
	s.fake.NextChargeFails(providers.ReasonDeclined)
	s.closePeriods(closingTime)
	invoiceID := s.invoices(teamID, owner.Token)[0]["id"].(string)
	s.Require().Equal("payment_failed", s.invoiceDetail(teamID, owner.Token, invoiceID).str("status"))

	s.assertError(s.pay(paidTeam, paidOwner.Token, paidID, ""), http.StatusConflict, int(invoice.Domain), invoice.ErrorNotPayable.Code)
	s.assertError(s.pay(teamID, owner.Token, invoiceID, fmt.Sprintf(`{"payment_method_id":%q}`, uuid.NewString())),
		http.StatusNotFound, int(paymentmethod.Domain), paymentmethod.ErrorNotFound.Code)
	s.assertError(s.pay(teamID, owner.Token, uuid.NewString(), ""), http.StatusNotFound, int(invoice.Domain), invoice.ErrorNotFound.Code)

	for attempt := range 5 {
		s.fake.NextChargeFails(providers.ReasonDeclined)
		res := s.pay(teamID, owner.Token, invoiceID, "")
		s.Require().Equal(http.StatusAccepted, res.Code, "attempt %d: %s", attempt+1, res.Raw)
		s.Equal("failed", res.str("status"))
		s.Equal("card_declined", res.str("failure_reason"))
	}
	limited := s.pay(teamID, owner.Token, invoiceID, "")
	s.assertError(limited, http.StatusTooManyRequests, int(payment.Domain), payment.ErrorPayRateLimited.Code)
	s.Equal("3600", limited.Hdr.Get("Retry-After"))
	s.Len(s.objectList(s.invoiceDetail(teamID, owner.Token, invoiceID).Body["payments"]), 6, "the refused attempt never reached the provider")
}

func (s *billingSuite) TestCardExpiring_WarnsOncePerDefaultCard() {
	s.clock.Set(oct(5, 9, 0))
	owner := s.signIn()
	teamID := s.team(owner)
	s.Require().Equal(http.StatusOK, s.putProfile(teamID, owner.Token, deCompany()).Code)
	s.fake.NextCard("visa", "1111", 10, 2026)
	s.addCard(teamID, owner.Token)
	s.fake.NextCard("mastercard", "2222", 10, 2026)
	s.addCard(teamID, owner.Token)
	noExpiryOwner := s.signIn()
	noExpiry := s.team(noExpiryOwner)
	s.Require().Equal(http.StatusOK, s.putProfile(noExpiry, noExpiryOwner.Token, deCompany()).Code)
	s.fake.NextCard("visa", "3333", 0, 0)
	s.addCard(noExpiry, noExpiryOwner.Token)
	laterOwner := s.signIn()
	later := s.team(laterOwner)
	s.Require().Equal(http.StatusOK, s.putProfile(later, laterOwner.Token, deCompany()).Code)
	s.fake.NextCard("visa", "4444", 12, 2027)
	s.addCard(later, laterOwner.Token)

	s.Require().NoError(s.jobs.CardExpiring(context.Background()))
	s.Require().NoError(s.jobs.CardExpiring(context.Background()))

	warnings := s.outboxPayloads(constants.CardExpiring)
	s.Require().Len(warnings, 1, "the default card only, once; no expiry and far expiries are skipped")
	s.Equal("visa", warnings[0]["brand"])
	s.Equal("1111", warnings[0]["last4"])
	s.Equal(float64(10), warnings[0]["exp_month"])
	s.Equal(float64(2026), warnings[0]["exp_year"])
	s.Equal(teamID.String(), warnings[0]["team_id"])
	s.NotEmpty(warnings[0]["payment_method_id"])
	s.Equal("books@acme.example", warnings[0]["invoice_address"].(map[string]any)["email"])
}

// Craftgate reports no expiry with a saved card. The provider's own verdict
// stands in for it, and an expiry it does report is stored.
func (s *billingSuite) TestCardExpiring_AsksTheProviderAboutCardsWithoutAnExpiry() {
	s.clock.Set(oct(5, 9, 0))
	owner := s.signIn()
	teamID := s.team(owner)
	s.Require().Equal(http.StatusOK, s.putProfile(teamID, owner.Token, deCompany()).Code)
	s.fake.NextCard("mastercard", "7777", 0, 0)
	s.addCard(teamID, owner.Token)
	s.fake.SetCardStatus(s.cardRef(teamID), providers.CardStatus{ExpiringSoon: true})
	reportedOwner := s.signIn()
	reported := s.team(reportedOwner)
	s.Require().Equal(http.StatusOK, s.putProfile(reported, reportedOwner.Token, deCompany()).Code)
	s.fake.NextCard("visa", "8888", 0, 0)
	s.addCard(reported, reportedOwner.Token)
	s.fake.SetCardStatus(s.cardRef(reported), providers.CardStatus{ExpMonth: 10, ExpYear: 2026})

	s.Require().NoError(s.jobs.CardExpiring(context.Background()))
	s.Require().NoError(s.jobs.CardExpiring(context.Background()))

	warnings := s.outboxPayloads(constants.CardExpiring)
	s.Require().Len(warnings, 2, "once per card")
	byLast4 := map[string]map[string]any{}
	for _, w := range warnings {
		byLast4[w["last4"].(string)] = w
	}
	s.Equal(float64(11), byLast4["7777"]["exp_month"], "the provider's \"expires next month\" is November")
	s.Equal(float64(2026), byLast4["7777"]["exp_year"])
	s.Equal(float64(10), byLast4["8888"]["exp_month"])
	stored := s.get(billingPath(reported, "/payment-methods"), reportedOwner.Token).List[0]
	s.Equal(float64(10), stored["exp_month"], "a reported expiry is kept")
	s.Equal(float64(2026), stored["exp_year"])
	unknown := s.get(billingPath(teamID, "/payment-methods"), owner.Token).List[0]
	s.Equal(float64(0), unknown["exp_month"], "a verdict is not an expiry")
}
