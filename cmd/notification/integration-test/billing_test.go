package integrationtest

import (
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/notification/async/constants"
	"github.com/piggystats/nodera/contexts/notification/email"
)

const invoiceAddress = "books@acme.example"

// billingEvent is the billing context publishing one of its events for the
// Team, naming its profile's invoice address.
func (s *notificationSuite) billingEvent(routingKey string, teamID uuid.UUID, fields map[string]any) error {
	payload := map[string]any{
		"team_id":         teamID,
		"invoice_address": map[string]any{"email": invoiceAddress, "full_name": "Acme GmbH", "locale": "en"},
	}
	for k, v := range fields {
		payload[k] = v
	}

	return s.consumeErr(routingKey, s.json(map[string]any{"payload": payload}))
}

func invoiceIssued(invoiceID uuid.UUID) map[string]any {
	return map[string]any{
		"invoice_id": invoiceID, "invoice_number": "NOD-2026-000042", "total_due": "120.00", "currency": "USD",
		"period_start": "2026-10-01T00:00:00Z", "period_end": "2026-10-31T23:59:59Z",
		"pdf_url": "http://localhost:3002/billing/invoices/x/pdf?expires=1&token=secret-pdf-token",
	}
}

func (s *notificationSuite) TestInvoiceIssued_ReachesWhoeverMaySeeTheTeamsBillingAndTheInvoiceAddress() {
	ayse, zeynep, mehmet := s.signIn("Ayse", "tr"), s.signIn("Zeynep", "en"), s.signIn("Mehmet", "tr")
	teamID := s.team("Acme", ayse)
	s.addMember(teamID, zeynep, "finance")
	s.addMember(teamID, mehmet, "developer")
	invoiceID := uuid.New()

	s.Require().NoError(s.billingEvent(constants.InvoiceIssued, teamID, invoiceIssued(invoiceID)))

	for _, member := range []user{ayse, zeynep} {
		items := s.inbox(member)
		s.Require().Len(items, 1, member.FullName)
		s.Equal("invoice_issued", items[0]["type"])
		s.Equal(teamID.String(), items[0]["team_id"])
		params := items[0]["params"].(map[string]any)
		s.Equal(invoiceID.String(), params["invoice_id"])
		s.Equal("NOD-2026-000042", params["invoice_number"])
		s.Equal("120.00", params["total_due"])
		s.NotContains(params, "pdf_url", "a signed link is never recorded")
		s.Len(s.emailsTo(member), 1, member.FullName)
	}
	s.Empty(s.inbox(mehmet), "a developer holds no billing.read")
	s.Empty(s.emailsTo(mehmet))

	sent := s.emailsTo(user{Email: invoiceAddress})
	s.Require().Len(sent, 1, "the invoice address is told by email although no member holds it")
	s.Equal(email.KindInvoiceIssued, sent[0].Kind)
	s.Equal("Acme GmbH", sent[0].Params["FullName"])
	s.Equal("Acme", sent[0].Params["TeamName"])
	s.Contains(sent[0].Params["PdfURL"], "secret-pdf-token", "the email still carries the link")
	s.Equal(0, s.recordedContaining("secret-pdf-token"))
}

func (s *notificationSuite) TestBilling_TheInvoiceAddressIsToldOnceWhenAMemberHoldsIt() {
	ayse := s.signIn("Ayse", "tr")
	ayse.Email = invoiceAddress
	teamID := s.team("Acme", ayse)

	s.Require().NoError(s.billingEvent(constants.InvoiceIssued, teamID, invoiceIssued(uuid.New())))

	sent := s.emailsTo(ayse)
	s.Require().Len(sent, 1)
	s.Equal("tr", sent[0].Locale, "the member keeps their own language")
}

func (s *notificationSuite) TestBilling_ARedeliveredEventIsToldOnce() {
	ayse := s.signIn("Ayse", "tr")
	teamID := s.team("Acme", ayse)
	paymentID := uuid.New()
	failed := map[string]any{
		"invoice_id": uuid.New(), "payment_id": paymentID, "invoice_number": "NOD-1", "amount": "10.00", "currency": "USD",
		"failure_reason": "insufficient funds", "attempt": 1, "next_retry_at": time.Now().Add(48 * time.Hour).UTC(),
	}

	s.Require().NoError(s.billingEvent(constants.PaymentFailed, teamID, failed))
	s.Require().NoError(s.billingEvent(constants.PaymentFailed, teamID, failed))

	s.Len(s.inbox(ayse), 1)
	s.Len(s.emailsTo(ayse), 1)
	s.Len(s.emailsTo(user{Email: invoiceAddress}), 1)

	failed["payment_id"], failed["attempt"] = uuid.New(), 2
	s.Require().NoError(s.billingEvent(constants.PaymentFailed, teamID, failed))
	s.Len(s.inbox(ayse), 2, "the next attempt is a new failure")
}

func (s *notificationSuite) TestPaymentConfirmationRequired_KeepsTheLinkOutOfTheRecord() {
	ayse := s.signIn("Ayse", "tr")
	teamID := s.team("Acme", ayse)

	s.Require().NoError(s.billingEvent(constants.PaymentConfirmationRequired, teamID, map[string]any{
		"invoice_id": uuid.New(), "payment_id": uuid.New(), "invoice_number": "NOD-1", "amount": "10.00", "currency": "USD",
		"confirm_url": "http://localhost:3002/billing/payments/x/confirm?token=secret-confirm-token",
	}))

	s.Len(s.inbox(ayse), 1)
	s.Contains(s.emailsTo(ayse)[0].Params["ConfirmURL"], "secret-confirm-token")
	s.Equal(0, s.recordedContaining("secret-confirm-token"))
}

func (s *notificationSuite) TestDropletsDeletionWarning_BothWarningsOfOneInvoiceAreTold() {
	ayse := s.signIn("Ayse", "tr")
	teamID := s.team("Acme", ayse)
	invoiceID := uuid.New()
	for _, day := range []int{14, 20} {
		s.Require().NoError(s.billingEvent(constants.DropletsDeletionWarning, teamID, map[string]any{
			"invoice_id": invoiceID, "invoice_number": "NOD-1", "dunning_day": day,
			"deletion_at": time.Now().Add(7 * 24 * time.Hour).UTC(), "pay_url": "http://localhost:3002/dashboard/team/billing",
		}))
	}

	s.Len(s.inbox(ayse), 2)
	s.Len(s.emailsTo(ayse), 2)
}

func (s *notificationSuite) TestBilling_AnEventWithoutItsTeamIsRefused() {
	s.Error(s.consumeErr(constants.TeamSuspended, `{"payload":{"invoice_id":"`+uuid.NewString()+`","invoice_number":"NOD-1"}}`))
}

// recordedContaining counts recorded Notifications whose params hold the text.
func (s *notificationSuite) recordedContaining(text string) int {
	var count int
	s.Require().NoError(s.pgPool.QueryRow(s.T().Context(), `SELECT count(*) FROM notifications WHERE params::text LIKE '%' || $1 || '%'`, text).Scan(&count))

	return count
}
