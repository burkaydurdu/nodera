package integrationtest

import (
	"context"
	"net/http"
	"time"

	"github.com/piggystats/nodera/contexts/billing/aggregates/billingstatus"
	"github.com/piggystats/nodera/contexts/billing/application/jobs"
	"github.com/piggystats/nodera/pkg/realtime"
)

func (s *billingSuite) TestStatus_DefaultsToNoPaymentMethodWithEveryFieldNull() {
	owner := s.signIn()
	teamID := s.team(owner)

	res := s.get(billingPath(teamID, "/status"), owner.Token)

	s.Require().Equal(http.StatusOK, res.Code, res.Raw)
	s.Equal("no_payment_method", res.str("status"))
	for _, field := range []string{"open_invoice_id", "next_retry_at", "suspended_at", "deletion_at"} {
		value, present := res.Body[field]
		s.True(present, field)
		s.Nil(value, field)
	}
}

// Collection owns suspension; the endpoint answers whatever the row holds.
func (s *billingSuite) TestStatus_AnswersTheCollectionFields() {
	owner := s.signIn()
	teamID := s.team(owner)
	ctx := context.Background()
	suspendedAt := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	deletionAt := suspendedAt.AddDate(0, 0, 14)

	current, err := s.services.Statuses.FindForUpdate(ctx, teamID, suspendedAt)
	s.Require().NoError(err)
	current.Status, current.ChangedAt, current.SuspendedAt, current.DeletionAt = billingstatus.Suspended, suspendedAt, &suspendedAt, &deletionAt
	s.Require().NoError(s.services.Statuses.Save(ctx, current))

	res := s.get(billingPath(teamID, "/status"), owner.Token)

	s.Equal("suspended", res.str("status"))
	s.Equal("2026-11-08T00:00:00Z", res.str("suspended_at"))
	s.Equal("2026-11-22T00:00:00Z", res.str("deletion_at"))
}

// A suspended Team that adds a card stays suspended: only collection lifts it.
func (s *billingSuite) TestStatus_NewCardDoesNotLiftASuspension() {
	owner, teamID := s.payingTeam()
	ctx := context.Background()
	now := time.Now().UTC()
	current, err := s.services.Statuses.FindForUpdate(ctx, teamID, now)
	s.Require().NoError(err)
	current.Status, current.SuspendedAt = billingstatus.Suspended, &now
	s.Require().NoError(s.services.Statuses.Save(ctx, current))

	s.addCard(teamID, owner.Token)

	s.Equal("suspended", s.get(billingPath(teamID, "/status"), owner.Token).str("status"))
	s.Empty(s.statusEvents(teamID))
}

func (s *billingSuite) TestLock_SecondHolderSkipsAndFirstReleases() {
	ctx := context.Background()
	const key = "billing:lock:test"
	defer s.deleteByPattern(key)
	locker := jobs.NewLocker(s.redis)

	ran, err := locker.WithLock(ctx, key, time.Minute, func(ctx context.Context) error {
		nested, err := locker.WithLock(ctx, key, time.Minute, func(context.Context) error { return nil })
		s.Require().NoError(err)
		s.False(nested, "a run that finds the lock taken skips its turn")

		return nil
	})
	s.Require().NoError(err)
	s.True(ran)

	exists, err := s.redis.Exists(ctx, key)
	s.Require().NoError(err)
	s.False(exists, "released once the run finished")
}

func (s *billingSuite) TestStatus_EveryChangeTellsTheTeamLive() {
	owner, teamID := s.payingTeam()
	s.addCard(teamID, owner.Token)

	live := make([]map[string]any, 0)
	for _, payload := range s.outboxPayloads(realtime.RoutingKey) {
		if payload["type"] == "billing.status_changed" {
			live = append(live, payload)
		}
	}
	s.Require().Len(live, 1, "the first card made the Team active")
	address, _ := live[0]["address"].(map[string]any)
	s.Equal(teamID.String(), address["team_id"])
	s.Equal("team.read", address["permission"], "every member sees why spending is blocked")
	s.Equal(map[string]any{"team_id": teamID.String(), "status": "active"}, live[0]["data"])
}
