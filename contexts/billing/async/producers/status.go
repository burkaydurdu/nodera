package producers

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/billing/async/constants"
	"github.com/piggystats/nodera/contexts/billing/async/events"
	"github.com/piggystats/nodera/pkg/apperror"
	"github.com/piggystats/nodera/pkg/authz"
	"github.com/piggystats/nodera/pkg/realtime"
)

// LiveStatusChanged is the realtime message type the web app refetches a
// Team's Billing status on.
const LiveStatusChanged = "billing.status_changed"

type liveStatus struct {
	TeamID string `json:"team_id"`
	Status string `json:"status"`
}

// Timestamps go out in UTC so every consumer reads the same instant whatever
// zone the database handed back.

// TeamBillingStatusChanged tells the droplet context, and nudges the Team's
// open web apps to refetch (docs/adr/0007). The status is team.read so that
// every member, not only finance, sees why spending is blocked.
type TeamBillingStatusChanged struct{ client OutboxClient }

func NewTeamBillingStatusChanged(client OutboxClient) *TeamBillingStatusChanged {
	return &TeamBillingStatusChanged{client: client}
}

func (p *TeamBillingStatusChanged) Publish(ctx context.Context, teamID uuid.UUID, status string, changedAt time.Time) error {
	err := Publish(ctx, p.client, constants.TeamBillingStatusChanged, &events.TeamBillingStatusChangedEvent{
		Payload: events.TeamBillingStatusChangedBody{TeamID: teamID, Status: status, ChangedAt: changedAt.UTC()},
	})
	if err != nil {
		return err
	}

	message, err := realtime.New(realtime.ToTeam(teamID, authz.TeamRead), LiveStatusChanged, liveStatus{TeamID: teamID.String(), Status: status}, changedAt)
	if err != nil {
		return apperror.ErrorCommon.Copy().SetError(err)
	}

	return Publish(ctx, p.client, realtime.RoutingKey, message)
}

type TeamDropletsDeletionRequested struct{ client OutboxClient }

func NewTeamDropletsDeletionRequested(client OutboxClient) *TeamDropletsDeletionRequested {
	return &TeamDropletsDeletionRequested{client: client}
}

func (p *TeamDropletsDeletionRequested) Publish(ctx context.Context, teamID uuid.UUID, requestedAt time.Time) error {
	return Publish(ctx, p.client, constants.TeamDropletsDeletionRequested, &events.TeamDropletsDeletionRequestedEvent{
		Payload: events.TeamDropletsDeletionRequestedBody{TeamID: teamID, RequestedAt: requestedAt.UTC()},
	})
}
