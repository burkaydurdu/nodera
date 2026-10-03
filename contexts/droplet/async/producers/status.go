package producers

import (
	"context"

	"github.com/piggystats/nodera/contexts/droplet/aggregates/droplet"
	"github.com/piggystats/nodera/contexts/droplet/async/constants"
	"github.com/piggystats/nodera/contexts/droplet/async/events"
	"github.com/piggystats/nodera/pkg/apperror"
	"github.com/piggystats/nodera/pkg/authz"
	"github.com/piggystats/nodera/pkg/realtime"
)

// LiveStatusChanged is the realtime message type the web app refreshes a
// Droplet on.
const LiveStatusChanged = "droplet.status_changed"

type liveStatus struct {
	TeamID    string `json:"team_id"`
	DropletID string `json:"droplet_id"`
	ProjectID string `json:"project_id"`
	Status    string `json:"status"`
}

// StatusChanged tells the notification context what happened to a Droplet
// and the Team's open web apps that it changed; the second is only a nudge
// to refetch (docs/adr/0007), so it carries no more than the web app needs to
// know which screen is stale.
type StatusChanged struct{ client OutboxClient }

func NewStatusChanged(client OutboxClient) *StatusChanged {
	return &StatusChanged{client: client}
}

func (p *StatusChanged) Publish(ctx context.Context, d *droplet.Droplet, from *droplet.Status) error {
	body := events.DropletStatusChangedBody{
		DropletID: d.ID,
		TeamID:    d.TeamID,
		ProjectID: d.ProjectID,
		Hostname:  d.Hostname,
		Status:    string(d.Status),
		ChangedAt: d.UpdatedAt.UTC(),
	}
	if from != nil {
		previous := string(*from)
		body.FromStatus = &previous
	}
	if d.IPv4 != nil {
		body.IPv4 = *d.IPv4
	}
	if d.Status == droplet.StatusFailed && d.FailureReason != nil {
		body.FailureReason = *d.FailureReason
	}
	if d.Status == droplet.StatusFailed && d.FailureCode != nil {
		body.FailureCode = string(*d.FailureCode)
	}
	if err := publish(ctx, p.client, constants.DropletStatusChanged, &events.DropletStatusChangedEvent{Payload: body}); err != nil {
		return err
	}

	message, err := realtime.New(realtime.ToTeam(d.TeamID, authz.DropletRead), LiveStatusChanged, liveStatus{
		TeamID: d.TeamID.String(), DropletID: d.ID.String(), ProjectID: d.ProjectID.String(), Status: string(d.Status),
	}, body.ChangedAt)
	if err != nil {
		return apperror.ErrorCommon.Copy().SetError(err)
	}

	return publish(ctx, p.client, realtime.RoutingKey, message)
}
