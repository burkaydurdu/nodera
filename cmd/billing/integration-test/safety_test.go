package integrationtest

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"go.mongodb.org/mongo-driver/bson"

	"github.com/piggystats/nodera/cmd/billing/app"
	"github.com/piggystats/nodera/contexts/billing/aggregates/invoice"
	"github.com/piggystats/nodera/contexts/billing/async/constants"
	"github.com/piggystats/nodera/contexts/billing/providers"
)

// Two people press "Pay now" at once: the second is told a payment is in
// progress instead of starting a second charge.
func (s *billingSuite) TestPay_ConcurrentPayNowMakesOneAttempt() {
	owner, teamID := s.germanTeam()
	s.droplet(teamID, "web-1", "1.0000", oct(5, 0, 0), at(oct(5, 10, 0)))
	s.fake.NextChargeFails(providers.ReasonDeclined)
	s.closePeriods(closingTime)
	invoiceID := s.invoices(teamID, owner.Token)[0]["id"].(string)
	entered, release := s.fake.HoldNextCharge()
	defer release()

	first := make(chan *response, 1)
	go func() { first <- s.pay(teamID, owner.Token, invoiceID, "") }()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		s.FailNow("the first attempt never reached the provider")
	}
	second := s.pay(teamID, owner.Token, invoiceID, "")
	release()
	paid := <-first

	s.assertError(second, http.StatusConflict, int(invoice.Domain), invoice.ErrorPaymentInProgress.Code)
	s.Require().Equal(http.StatusAccepted, paid.Code, paid.Raw)
	s.Equal("succeeded", paid.str("status"))
	s.Len(s.objectList(s.invoiceDetail(teamID, owner.Token, invoiceID).Body["payments"]), 2, "the failed charge at issue and one pay-now")
	s.Len(s.fake.Charges, 2)
	s.Equal(1, s.fake.Captures())
}

// A retry is skipped while the cardholder's confirmation link is open; the
// schedule still suspends on day 7, and the lapsed link fails the attempt.
func (s *billingSuite) TestDunning_SkipsARetryWhileAConfirmationIsOpen() {
	owner, teamID := s.germanTeam()
	s.droplet(teamID, "web-1", "1.0000", oct(5, 0, 0), at(oct(5, 10, 0)))
	s.fake.NextChargeFails(providers.ReasonDeclined)
	s.closePeriods(closingTime)
	day := func(n int) time.Time { return closingTime.AddDate(0, 0, n) }

	s.fake.NextChargeNeedsConfirmation()
	s.runDunning(day(1))
	s.Len(s.outboxPayloads(constants.PaymentConfirmationRequired), 1)

	s.runDunning(day(3))
	s.runDunning(day(7))

	s.Len(s.fake.Charges, 2, "no retry while the link is open")
	s.Equal([]string{"active", "suspended"}, s.statusEvents(teamID), "the schedule suspends on its own")
	s.Len(s.outboxPayloads(constants.TeamSuspended), 1)

	s.sweep(day(8))
	failures := s.outboxPayloads(constants.PaymentFailed)
	s.Require().Len(failures, 2)
	s.Equal(float64(2), failures[1]["attempt"])
	s.Equal("confirmation_expired", failures[1]["failure_reason"])
	s.Equal("suspended", s.get(billingPath(teamID, "/status"), owner.Token).str("status"))
}

// A provider that does not answer the day-7 retry does not keep the Team
// out of suspension, and the deletion request goes to a suspended Team.
func (s *billingSuite) TestDunning_ProviderSilentOnDay7StillSuspends() {
	owner, teamID := s.germanTeam()
	s.droplet(teamID, "web-1", "1.0000", oct(5, 0, 0), at(oct(5, 10, 0)))
	for range 3 {
		s.fake.NextChargeFails(providers.ReasonDeclined)
	}
	s.closePeriods(closingTime)
	day := func(n int) time.Time { return closingTime.AddDate(0, 0, n) }
	s.runDunning(day(1))
	s.runDunning(day(3))

	s.fake.FailNextCharge(errors.New("payment provider create payment: timeout"))
	s.runDunning(day(7))

	s.Len(s.outboxPayloads(constants.PaymentFailed), 3, "the unanswered retry sends no failure email")
	s.Equal([]string{"active", "suspended"}, s.statusEvents(teamID))
	s.Len(s.outboxPayloads(constants.TeamSuspended), 1)
	status := s.get(billingPath(teamID, "/status"), owner.Token)
	s.Equal("suspended", status.str("status"))
	s.Equal(rfc3339(day(21)), status.str("deletion_at"))

	s.runDunning(day(14))
	s.Len(s.outboxPayloads(constants.DropletsDeletionWarning), 1)
	s.runDunning(day(21))
	deletions := s.outboxPayloads(constants.TeamDropletsDeletionRequested)
	s.Require().Len(deletions, 1)
	s.Equal(teamID.String(), deletions[0]["team_id"])
}

// An issued Invoice nobody charged (the process stopped between the issue
// and the charge) is charged by the sweep, and its issue email is sent if it
// never went out, once.
func (s *billingSuite) TestSweep_ChargesAnIssuedInvoiceNobodyCharged() {
	owner, teamID := s.germanTeam()
	s.droplet(teamID, "web-1", "1.0000", oct(5, 0, 0), at(oct(5, 10, 0)))
	s.closePeriods(closingTime)
	invoiceID := s.invoices(teamID, owner.Token)[0]["id"].(string)
	s.forgetCollection(invoiceID)

	s.sweep(closingTime.Add(10 * time.Minute))
	s.Empty(s.outboxPayloads(constants.InvoiceIssued), "an Invoice younger than 15 minutes is still being collected")

	s.sweep(closingTime.Add(16 * time.Minute))
	s.sweep(closingTime.Add(30 * time.Minute))

	s.Len(s.outboxPayloads(constants.InvoiceIssued), 1, "sent once")
	detail := s.invoiceDetail(teamID, owner.Token, invoiceID)
	s.Equal("paid", detail.str("status"))
	s.Len(s.objectList(detail.Body["payments"]), 1)
	s.Len(s.outboxPayloads(constants.PaymentSucceeded), 1)
}

// forgetCollection rewinds an Invoice to the moment after its issue
// committed: no attempt, no email.
func (s *billingSuite) forgetCollection(invoiceID string) {
	ctx := context.Background()
	_, err := s.pgPool.Exec(ctx, `DELETE FROM payments WHERE invoice_id = $1`, invoiceID)
	s.Require().NoError(err)
	_, err = s.pgPool.Exec(ctx, `UPDATE invoices SET status = 'issued', paid_at = NULL, notified_at = NULL WHERE id = $1`, invoiceID)
	s.Require().NoError(err)
	_, err = s.mongoDB.Collection("outbox").DeleteMany(ctx, bson.M{})
	s.Require().NoError(err)
}

// A confirmation link that lapses fails its attempt, which starts Dunning.
func (s *billingSuite) TestSweep_ALapsedConfirmationStartsDunning() {
	owner, teamID := s.germanTeam()
	s.droplet(teamID, "web-1", "1.0000", oct(5, 0, 0), at(oct(5, 10, 0)))
	s.fake.NextChargeNeedsConfirmation()
	s.closePeriods(closingTime)
	lapse := closingTime.Add(7 * 24 * time.Hour)

	s.sweep(lapse.Add(-time.Minute))
	s.Equal("issued", s.onlyInvoice(teamID, owner.Token).str("status"))

	s.sweep(lapse)

	detail := s.onlyInvoice(teamID, owner.Token)
	s.Equal("payment_failed", detail.str("status"))
	s.Equal("failed", s.objectList(detail.Body["payments"])[0]["status"])
	failures := s.outboxPayloads(constants.PaymentFailed)
	s.Require().Len(failures, 1)
	s.Equal("confirmation_expired", failures[0]["failure_reason"])
	s.Equal(rfc3339(lapse.AddDate(0, 0, 1)), failures[0]["next_retry_at"])
	s.Equal(detail.str("id"), s.get(billingPath(teamID, "/status"), owner.Token).str("open_invoice_id"))

	s.runDunning(lapse.AddDate(0, 0, 1))
	s.Len(s.fake.Charges, 2, "the day-1 retry")
}

// Usage before billing went live (the suite's cutover is 15 September) is
// never billed.
func (s *billingSuite) TestCutover_UsageBeforeTheStartIsNotBilled() {
	owner, teamID := s.germanTeam()
	s.droplet(teamID, "across-1", "1.0000", time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC), at(time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)))
	s.droplet(teamID, "before-1", "1.0000", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), at(time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)))

	s.closePeriods(october.Add(5 * time.Minute))

	detail := s.onlyInvoice(teamID, owner.Token)
	lines := s.objectList(detail.Body["lines"])
	s.Require().Len(lines, 1, "a Droplet that only ran before the cutover is not on it")
	s.Equal("across-1", lines[0]["hostname"])
	s.Equal(float64(24), lines[0]["hours"], "from the cutover, not from its start six days earlier")
	s.Equal("24.00", detail.str("total_due"))
}

// replay-status republishes every Team's current status with the row's own
// change time, so the droplet context's last-writer-wins model keeps it.
func (s *billingSuite) TestReplayStatus_RepublishesEveryTeamsCurrentStatus() {
	_, active := s.germanTeam()
	_, blocked := s.payingTeam()
	var activeAt time.Time
	s.Require().NoError(s.pgPool.QueryRow(context.Background(), `SELECT changed_at FROM team_billing_status WHERE team_id = $1`, active).Scan(&activeAt))
	_, err := s.mongoDB.Collection("outbox").DeleteMany(context.Background(), bson.M{})
	s.Require().NoError(err)

	count, err := app.ReplayStatuses(context.Background(), s.deps)

	s.Require().NoError(err)
	s.Equal(2, count, fmt.Sprintf("teams %s and %s", active, blocked))
	s.Equal([]string{"active"}, s.statusEvents(active))
	s.Equal([]string{"no_payment_method"}, s.statusEvents(blocked))
	for _, p := range s.outboxPayloads(constants.TeamBillingStatusChanged) {
		if p["team_id"] == active.String() {
			s.Equal(activeAt.UTC().Format(time.RFC3339Nano), p["changed_at"])
		}
	}
}
