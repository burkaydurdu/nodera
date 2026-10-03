package integrationtest

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/piggystats/nodera/contexts/billing/aggregates/access"
	"github.com/piggystats/nodera/contexts/billing/async/constants"
)

func (s *billingSuite) TestClosePeriod_OneInvoicePerTeamNumberedInSequence() {
	ownerA, teamA := s.germanTeam()
	ownerB, teamB := s.germanTeam()
	ownerC, teamC := s.germanTeam()
	ownerD, teamD := s.germanTeam()
	s.droplet(teamA, "a-1", "1.0000", oct(10, 0, 0), at(oct(10, 10, 0)))
	s.droplet(teamB, "b-1", "2.0000", oct(20, 0, 0), at(oct(20, 5, 0)))
	s.droplet(teamC, "c-1", "5.0000", oct(12, 10, 0), at(oct(12, 10, 0)))
	s.droplet(teamD, "d-1", "1.0000", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), at(time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)))

	s.closePeriods(closingTime)
	s.closePeriods(closingTime.Add(10 * time.Minute))

	a := s.invoices(teamA, ownerA.Token)
	b := s.invoices(teamB, ownerB.Token)
	s.Require().Len(a, 1, "closing twice issues once")
	s.Require().Len(b, 1)
	s.Empty(s.invoices(teamC, ownerC.Token), "a zero total is not issued")
	s.Empty(s.invoices(teamD, ownerD.Token), "no usage in the Period")
	s.ElementsMatch([]any{"NDR-2026-000001", "NDR-2026-000002"}, []any{a[0]["number"], b[0]["number"]})

	detail := s.invoiceDetail(teamA, ownerA.Token, a[0]["id"].(string))
	s.Equal("period", detail.str("kind"))
	s.Equal("paid", detail.str("status"), "the default card was charged on issue")
	s.Equal("2026-10-01T00:00:00Z", detail.str("period_start"))
	s.Equal("2026-11-01T00:00:00Z", detail.str("period_end"))
	s.Equal(rfc3339(closingTime), detail.str("issued_at"))
	s.Equal(rfc3339(closingTime), detail.str("paid_at"))
	s.Equal("USD", detail.str("currency"))
	s.Equal("10.00", detail.str("subtotal"))
	s.Equal("0.00", detail.str("tax_total"))
	s.Equal("0.00", detail.str("credit_applied"))
	s.Equal("10.00", detail.str("total_due"))
	s.Nil(detail.Body["settlement_currency"])
	s.Nil(detail.Body["settlement_amount"])
	s.Nil(detail.Body["exchange_rate"])
	lines := s.objectList(detail.Body["lines"])
	s.Require().Len(lines, 1)
	s.Equal("a-1", lines[0]["hostname"])
	s.Equal(float64(10), lines[0]["hours"])
	s.Equal("1.0000", lines[0]["hourly_price"])
	s.Equal("10.00", lines[0]["amount"])
	taxLines := s.objectList(detail.Body["tax_lines"])
	s.Require().Len(taxLines, 1)
	s.Equal("Export-service exemption", taxLines[0]["label"])
	s.Equal(float64(0), taxLines[0]["rate_bps"])
	s.Equal("10.00", taxLines[0]["base"])
	s.Equal("0.00", taxLines[0]["amount"])
	payments := s.objectList(detail.Body["payments"])
	s.Require().Len(payments, 1)
	s.Equal("succeeded", payments[0]["status"])
	s.Equal("10.00", payments[0]["amount"])
	s.Equal("USD", payments[0]["currency"])
	s.Len(s.outboxPayloads(constants.InvoiceIssued), 2)
	s.NotContains(strings.ToLower(detail.Raw), "craftgate", "no provider name in a DTO")
}

// A Turkish Team pays 20% KDV and is charged the TRY equivalent at the
// latest bulletin dated before the issue date: the bank publishes in the
// afternoon the rates for the next day, so the issue day's own bulletin is
// never used, whatever the hour of issue.
func (s *billingSuite) TestClosePeriod_TurkishTeamPaysKDVInTRYAtTheStoredRate() {
	owner, teamID := s.turkishTeam()
	s.storeRate(time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC), "41.5000")
	s.storeRate(time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC), "41.6512")
	s.storeRate(november, "99.9999")
	s.droplet(teamID, "web-1", "1.0000", oct(5, 0, 0), at(oct(9, 4, 0)))

	s.closePeriods(closingTime)

	detail := s.onlyInvoice(teamID, owner.Token)
	s.Equal("100.00", detail.str("subtotal"))
	s.Equal("20.00", detail.str("tax_total"))
	s.Equal("120.00", detail.str("total_due"))
	s.Equal("TRY", detail.str("settlement_currency"))
	s.Equal("4998.14", detail.str("settlement_amount"), "120 × 41.6512 = 4998.144")
	s.Equal("41.6512", detail.str("exchange_rate"))
	s.Equal("2026-10-30", detail.str("exchange_rate_date"), "the 1st's own bulletin is not used; Friday's is the latest before it")
	taxLines := s.objectList(detail.Body["tax_lines"])
	s.Require().Len(taxLines, 1)
	s.Equal("KDV %20", taxLines[0]["label"])
	s.Equal(float64(2000), taxLines[0]["rate_bps"])
	s.Equal("100.00", taxLines[0]["base"])
	s.Equal("20.00", taxLines[0]["amount"])

	s.Require().Len(s.fake.Charges, 1)
	charge := s.fake.Charges[0]
	s.Equal("4998.14", charge.Amount.StringFixed(2), "the TRY card is charged the settlement amount")
	s.Equal("TRY", charge.Currency)
	s.Equal("invoice:"+detail.str("id")+":attempt:1", charge.IdempotencyKey)

	issued := s.outboxPayloads(constants.InvoiceIssued)
	s.Require().Len(issued, 1)
	s.Equal(detail.str("number"), issued[0]["invoice_number"])
	s.Equal(teamID.String(), issued[0]["team_id"])
	s.Equal(detail.str("id"), issued[0]["invoice_id"])
	s.Equal(map[string]any{"email": "muhasebe@acme.example", "full_name": "Acme Bilişim A.Ş.", "locale": "tr"}, issued[0]["invoice_address"])
	s.Equal("120.00", issued[0]["total_due"])
	s.Equal("USD", issued[0]["currency"])
	s.Equal("4998.14", issued[0]["settlement_amount"])
	s.Equal("TRY", issued[0]["settlement_currency"])
	s.Equal("2026-10-01T00:00:00Z", issued[0]["period_start"])
	s.True(strings.HasPrefix(issued[0]["pdf_url"].(string), frontURL+"/billing/invoice-pdf?invoice="+detail.str("id")+"&expires="), issued[0]["pdf_url"])

	rendered := s.renderer.Rendered()
	s.Require().Len(rendered, 1)
	for _, want := range []string{"Hesap özeti", "fatura yerine geçmez", detail.str("number"), "Acme Bilişim A.Ş.", "1234567890", "Kadıköy", "KDV %20", "4998.14 TRY", "41.6512", "30.10.2026"} {
		s.Contains(rendered[0], want)
	}

	summary := s.invoices(teamID, owner.Token)[0]
	s.Equal("4998.14", summary["settlement_amount"])
	s.Equal("TRY", summary["settlement_currency"])
}

// A Turkish Team's Invoice cannot be issued without any rate; the next run
// issues it once one is stored.
func (s *billingSuite) TestClosePeriod_TurkishTeamWaitsForAnExchangeRate() {
	owner, teamID := s.turkishTeam()
	s.droplet(teamID, "web-1", "1.0000", oct(5, 0, 0), at(oct(5, 4, 0)))

	s.clock.Set(closingTime)
	s.Error(s.jobs.ClosePeriods(context.Background()))
	s.Empty(s.invoices(teamID, owner.Token))

	s.storeRate(november, "41.9000")
	s.clock.Set(closingTime.Add(5 * time.Minute))
	s.Error(s.jobs.ClosePeriods(context.Background()), "the issue date's own bulletin does not count")

	s.storeRate(time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC), "41.7000")
	s.closePeriods(closingTime.Add(10 * time.Minute))
	s.Equal("4.80", s.onlyInvoice(teamID, owner.Token).str("total_due"))
}

func (s *billingSuite) TestClosePeriod_CreditIsAppliedBeforeTheCard() {
	owner, teamID := s.germanTeam()
	s.grantCredit(teamID, "30.00")
	s.droplet(teamID, "web-1", "1.0000", oct(5, 0, 0), at(oct(9, 4, 0)))
	coveredOwner, covered := s.germanTeam()
	s.grantCredit(covered, "500.00")
	s.droplet(covered, "web-2", "1.0000", oct(5, 0, 0), at(oct(9, 4, 0)))

	s.closePeriods(closingTime)

	partial := s.onlyInvoice(teamID, owner.Token)
	s.Equal("100.00", partial.str("subtotal"))
	s.Equal("30.00", partial.str("credit_applied"))
	s.Equal("70.00", partial.str("total_due"))
	s.Equal("paid", partial.str("status"))

	full := s.onlyInvoice(covered, coveredOwner.Token)
	s.Equal("100.00", full.str("credit_applied"))
	s.Equal("0.00", full.str("total_due"))
	s.Equal("paid", full.str("status"), "Credit paid it")
	s.Empty(s.objectList(full.Body["payments"]), "nothing to charge")

	s.Require().Len(s.fake.Charges, 1, "only the partly covered Invoice reached a card")
	s.Equal("70.00", s.fake.Charges[0].Amount.StringFixed(2))

	balance, err := s.services.Invoices.CreditBalance(context.Background(), covered)
	s.Require().NoError(err)
	s.Equal("400.00", balance.StringFixed(2))
}

func (s *billingSuite) TestInvoices_FinanceReadsAndDeveloperIsForbidden() {
	_, teamID := s.germanTeam()
	s.droplet(teamID, "web-1", "1.0000", oct(5, 0, 0), at(oct(5, 4, 0)))
	s.closePeriods(closingTime)
	finance := s.signIn()
	s.addMember(teamID, finance, "finance", time.Now())
	developer := s.signIn()
	s.addMember(teamID, developer, "developer", time.Now())

	list := s.invoices(teamID, finance.Token)
	s.Require().Len(list, 1)
	id := list[0]["id"].(string)
	s.Equal(http.StatusOK, s.get(billingPath(teamID, "/invoices/"+id), finance.Token).Code)
	s.Equal(http.StatusOK, s.get(billingPath(teamID, "/invoices/"+id+"/pdf"), finance.Token).Code)

	for _, path := range []string{"/invoices", "/invoices/" + id, "/invoices/" + id + "/pdf"} {
		s.assertError(s.get(billingPath(teamID, path), developer.Token), http.StatusForbidden, int(access.Domain), access.ErrorForbidden.Code)
	}
	s.assertError(s.pay(teamID, developer.Token, id, ""), http.StatusForbidden, int(access.Domain), access.ErrorForbidden.Code)

	other, otherTeam := s.germanTeam()
	s.assertError(s.get(billingPath(otherTeam, "/invoices/"+id), other.Token), http.StatusNotFound, 33, 10001)
}
