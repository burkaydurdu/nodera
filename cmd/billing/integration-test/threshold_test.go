package integrationtest

import (
	"net/http"
	"time"

	"github.com/piggystats/nodera/contexts/billing/async/constants"
)

// A new Team is invoiced early once its unbilled usage reaches $50: 100
// hours at 0.5000 is exactly 50.00, at 0.4999 it is 49.99.
func (s *billingSuite) TestThreshold_NewTeamIsInvoicedAt50AndNotAt4999() {
	owner, teamID := s.germanTeam()
	s.droplet(teamID, "web-1", "0.5000", oct(1, 0, 0), nil)
	belowOwner, below := s.germanTeam()
	s.droplet(below, "web-2", "0.4999", oct(1, 0, 0), nil)

	s.checkThresholds(oct(5, 4, 0))
	s.checkThresholds(oct(5, 4, 0))

	invoices := s.invoices(teamID, owner.Token)
	s.Require().Len(invoices, 1, "the second check finds nothing unbilled")
	s.Equal("threshold", invoices[0]["kind"])
	s.Equal("50.00", invoices[0]["total_due"])
	s.Equal("NDR-2026-000001", invoices[0]["number"])
	s.Empty(s.invoices(below, belowOwner.Token))

	usage := s.usageOf(teamID, owner.Token)
	s.Empty(s.objectList(usage.Body["lines"]), "the draft starts again after a Threshold Invoice")
	s.Equal("0.00", usage.str("subtotal"))
	s.Equal("49.99", s.usageOf(below, belowOwner.Token).str("subtotal"))
}

// After three months a Team waits for $500.
func (s *billingSuite) TestThreshold_EstablishedTeamWaitsFor500() {
	owner := s.signIn()
	teamID := s.teamCreatedAt(owner, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
	s.Require().Equal(http.StatusOK, s.putProfile(teamID, owner.Token, deCompany()).Code)
	s.addCard(teamID, owner.Token)
	s.droplet(teamID, "web-1", "0.5000", oct(1, 0, 0), nil)

	s.checkThresholds(oct(5, 4, 0))
	s.Empty(s.invoices(teamID, owner.Token))
	threshold := s.usageOf(teamID, owner.Token).Body["threshold"].(map[string]any)
	s.Equal("500.00", threshold["amount"])
	s.Equal("established", threshold["kind"])

	s.droplet(teamID, "db-1", "4.5000", oct(1, 0, 0), nil)
	s.checkThresholds(oct(5, 4, 0))
	invoices := s.invoices(teamID, owner.Token)
	s.Require().Len(invoices, 1)
	s.Equal("500.00", invoices[0]["total_due"], "100 h × (0.50 + 4.50)")
}

// The Period Invoice bills only the hours the Threshold Invoice did not.
func (s *billingSuite) TestThreshold_ThenPeriodCloseDoesNotBillAnHourTwice() {
	owner, teamID := s.germanTeam()
	s.droplet(teamID, "web-1", "0.5000", oct(1, 0, 0), nil)

	s.checkThresholds(oct(5, 4, 0))
	s.closePeriods(closingTime)

	invoices := s.invoices(teamID, owner.Token)
	s.Require().Len(invoices, 2)
	period, threshold := invoices[0], invoices[1]
	s.Equal("period", period["kind"])
	s.Equal("NDR-2026-000002", period["number"])
	s.Equal("threshold", threshold["kind"])
	s.Equal("NDR-2026-000001", threshold["number"])

	periodLines := s.objectList(s.invoiceDetail(teamID, owner.Token, period["id"].(string)).Body["lines"])
	thresholdLines := s.objectList(s.invoiceDetail(teamID, owner.Token, threshold["id"].(string)).Body["lines"])
	s.Equal(float64(100), thresholdLines[0]["hours"])
	s.Equal(float64(572), periodLines[0]["hours"], "672 capped hours less the 100 already billed")
	s.Equal("286.00", period["total_due"])
	s.Len(s.outboxPayloads(constants.InvoiceIssued), 2)
}
