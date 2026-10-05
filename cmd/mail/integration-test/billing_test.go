package integrationtest

import (
	"fmt"
	"net/http"
	"time"

	"github.com/piggystats/nodera/contexts/mail/async/constants"
)

func (s *mailSuite) TestBilling_SuspensionStopsSendingAndPaymentResumesIt() {
	p, key := s.ready()

	s.setBilling(p.TeamID, "suspended", time.Now())
	s.expectError(s.sendTo(key, "no-reply@example.com", "ana@example.net"), http.StatusLocked, 51, 10004)
	s.Equal("suspended", s.overview(p).str("sending_state"))
	s.Contains(s.realtimeTypes(), constants.RealtimeSendingChanged)

	s.setBilling(p.TeamID, "active", time.Now())
	s.Require().Equal(http.StatusAccepted, s.sendTo(key, "no-reply@example.com", "ana@example.net").Code)
}

func (s *mailSuite) TestBilling_AnOlderStatusChangesNothing() {
	p, key := s.ready()

	s.setBilling(p.TeamID, "suspended", time.Now().Add(-time.Hour))
	s.Require().Equal(http.StatusAccepted, s.sendTo(key, "no-reply@example.com", "ana@example.net").Code)
}

// Day 21 of Dunning ends the Team's Packages so no new Period is charged;
// what the Team set up stays for when it comes back.
func (s *mailSuite) TestBilling_DeletionDayEndsThePackagesAndKeepsTheRest() {
	p, _ := s.ready()
	s.clock.Advance(time.Hour)

	s.consume(constants.TeamDropletsDeletionRequested, fmt.Sprintf(`{"payload":{"team_id":%q,"requested_at":%q}}`,
		p.TeamID, s.clock.Now().Format(time.RFC3339Nano)))

	stopped := s.outboxPayloads(constants.MailPackageStopped)
	s.Require().Len(stopped, 1)
	s.Equal(p.ProjectID.String(), stopped[0]["project_id"])
	s.sameInstant(s.clock.Now().Format(time.RFC3339Nano), stopped[0]["stopped_at"])

	o := s.overview(p)
	s.Nil(o.Body["package"])
	s.Len(s.manage(http.MethodGet, projectPath(p, "/domains"), "", p.Owner).List, 1)
}

// A cancellation due at the month's first instant ends the Package there,
// even when day 21 is consumed before the rollover job has run: the new
// month is not charged.
func (s *mailSuite) TestBilling_DeletionDayAfterTheBoundaryKeepsAScheduledCancellation() {
	p, _ := s.ready()
	s.Require().Equal(http.StatusOK, s.choose(p, "none").Code)

	s.clock.Set(time.Date(2026, 11, 1, 0, 5, 0, 0, time.UTC))
	s.consume(constants.TeamDropletsDeletionRequested, fmt.Sprintf(`{"payload":{"team_id":%q,"requested_at":%q}}`,
		p.TeamID, s.clock.Now().Format(time.RFC3339Nano)))

	stopped := s.outboxPayloads(constants.MailPackageStopped)
	s.Require().Len(stopped, 1)
	s.sameInstant("2026-11-01T00:00:00Z", stopped[0]["stopped_at"])
	s.Nil(s.overview(p).Body["package"])
}

func (s *mailSuite) TestBilling_MalformedEventsAreRefused() {
	s.Error(s.consumeErr(constants.TeamBillingStatusChanged, `{"payload":{"team_id":"00000000-0000-0000-0000-000000000000","status":"active","changed_at":"2026-10-12T10:00:00Z"}}`))
	s.Error(s.consumeErr(constants.TeamBillingStatusChanged, `{"payload":{"team_id":"0b1d2c3e-4f50-4a6b-8c7d-9e0f1a2b3c4d","status":"rich","changed_at":"2026-10-12T10:00:00Z"}}`))
	s.Error(s.consumeErr(constants.ProjectCreated, `not json`))
}
