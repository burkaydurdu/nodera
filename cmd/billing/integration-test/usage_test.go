package integrationtest

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/billing/aggregates/access"
	"github.com/piggystats/nodera/contexts/billing/aggregates/usage"
	"github.com/piggystats/nodera/contexts/billing/async/constants"
)

func (s *billingSuite) usageOf(teamID uuid.UUID, token string) *response {
	res := s.get(billingPath(teamID, "/usage"), token)
	s.Require().Equal(http.StatusOK, res.Code, res.Raw)

	return res
}

// Partial hours round up, a Droplet that ran all month is capped at 672, and
// the Period starts at midnight UTC on the 1st whatever started before.
func (s *billingSuite) TestUsage_RoundsUpCapsAndStartsAtThePeriod() {
	owner := s.signIn()
	teamID := s.team(owner)
	s.Require().Equal(http.StatusOK, s.putProfile(teamID, owner.Token, deCompany()).Code)
	allMonth := s.droplet(teamID, "db-1", "0.0744", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), nil)
	acrossMidnight := s.droplet(teamID, "edge-1", "0.1000", time.Date(2026, 9, 30, 23, 30, 0, 0, time.UTC), at(oct(1, 0, 30)))
	short := s.droplet(teamID, "web-1", "0.2766", oct(2, 10, 0), at(oct(2, 12, 1)))
	s.droplet(teamID, "old-1", "1.0000", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), at(time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)))
	s.clock.Set(oct(31, 23, 0))

	res := s.usageOf(teamID, owner.Token)

	s.Equal("2026-10-01T00:00:00Z", res.str("period_start"))
	s.Equal("2026-11-01T00:00:00Z", res.str("period_end"))
	lines := s.objectList(res.Body["lines"])
	s.Require().Len(lines, 3, "a Droplet stopped last month is not on this Period")
	s.Equal(allMonth.String(), lines[0]["droplet_id"])
	s.Equal(float64(usage.HourCap), lines[0]["hours"], "743 hours so far, capped")
	s.Equal("0.0744", lines[0]["hourly_price"])
	s.Equal("50.00", lines[0]["amount"], "672 × 0.0744 = 49.9968 rounds up")
	s.Equal(acrossMidnight.String(), lines[1]["droplet_id"])
	s.Equal(float64(1), lines[1]["hours"], "the half hour after midnight is a started hour")
	s.Equal("0.10", lines[1]["amount"])
	s.Equal(short.String(), lines[2]["droplet_id"])
	s.Equal("web-1", lines[2]["hostname"])
	s.Equal("c1-small", lines[2]["plan_code"])
	s.Equal(float64(3), lines[2]["hours"], "2 hours and a minute")
	s.Equal("0.83", lines[2]["amount"], "3 × 0.2766 = 0.8298 rounds up")
	s.Equal("50.93", res.str("subtotal"))
	s.Equal("0.00", res.str("estimated_tax"), "export-service exemption")
	s.Equal("50.93", res.str("estimated_total"))
	threshold := res.Body["threshold"].(map[string]any)
	s.Equal("50.00", threshold["amount"])
	s.Equal("new", threshold["kind"])
}

func (s *billingSuite) TestUsage_TurkishTeamIsEstimatedWithKDV() {
	owner, teamID := s.payingTeam()
	s.droplet(teamID, "web-1", "1.2345", oct(2, 0, 0), at(oct(2, 10, 0)))
	s.clock.Set(oct(20, 0, 0))

	res := s.usageOf(teamID, owner.Token)

	s.Equal("12.35", res.str("subtotal"), "10 × 1.2345 = 12.345 rounds up")
	s.Equal("2.47", res.str("estimated_tax"))
	s.Equal("14.82", res.str("estimated_total"))
}

func (s *billingSuite) TestUsage_WithoutAProfileNoTaxIsEstimated() {
	owner := s.signIn()
	teamID := s.team(owner)
	s.droplet(teamID, "web-1", "1.0000", oct(2, 0, 0), at(oct(2, 10, 0)))
	s.clock.Set(oct(20, 0, 0))

	res := s.usageOf(teamID, owner.Token)

	s.Equal("10.00", res.str("subtotal"))
	s.Equal("0.00", res.str("estimated_tax"))
	s.Equal("10.00", res.str("estimated_total"))
}

// The stop can overtake the start on the broker: it is kept as a tombstone,
// and the late start bills up to the stop rather than bringing the Droplet
// back. Redeliveries change nothing, the Locked price included.
func (s *billingSuite) TestUsage_StopBeforeStartIsATombstone() {
	owner := s.signIn()
	teamID := s.team(owner)
	dropletID := uuid.New()
	s.clock.Set(oct(20, 0, 0))

	s.stopped(teamID, dropletID, oct(2, 12, 0))
	s.Empty(s.objectList(s.usageOf(teamID, owner.Token).Body["lines"]), "a tombstone counts for nothing")

	s.started(teamID, dropletID, "web-1", "0.5000", oct(2, 10, 0))
	s.started(teamID, dropletID, "web-1", "9.9999", oct(1, 0, 0))
	s.stopped(teamID, dropletID, oct(3, 0, 0))
	s.stopped(teamID, uuid.New(), oct(2, 0, 0))

	lines := s.objectList(s.usageOf(teamID, owner.Token).Body["lines"])
	s.Require().Len(lines, 1, "a stop alone never makes a line")
	s.Equal(float64(2), lines[0]["hours"], "billed up to the first stop, not resurrected")
	s.Equal("0.5000", lines[0]["hourly_price"], "the first start's Locked price stays")
	s.Equal("1.00", lines[0]["amount"])
}

func (s *billingSuite) TestUsage_FinanceReadsAndDeveloperIsForbidden() {
	_, teamID := s.payingTeam()
	finance := s.signIn()
	s.addMember(teamID, finance, "finance", time.Now())
	developer := s.signIn()
	s.addMember(teamID, developer, "developer", time.Now())

	s.Equal(http.StatusOK, s.get(billingPath(teamID, "/usage"), finance.Token).Code)
	s.assertError(s.get(billingPath(teamID, "/usage"), developer.Token), http.StatusForbidden, int(access.Domain), access.ErrorForbidden.Code)
}

func (s *billingSuite) TestUsage_MalformedEventIsTerminal() {
	err := s.usage.Consume(rabbitMessage(constants.DropletBillingStarted,
		`{"payload":{"droplet_id":"`+uuid.NewString()+`","team_id":"`+uuid.NewString()+`","hourly_price":"cheap","started_at":"2026-10-02T10:00:00Z"}}`))
	s.Require().Error(err)
	s.ErrorIs(err, usage.ErrorMalformedEvent)

	err = s.usage.Consume(rabbitMessage(constants.DropletBillingStopped, `{"payload":{"droplet_id":"`+uuid.NewString()+`"}}`))
	s.ErrorIs(err, usage.ErrorMalformedEvent)
}
