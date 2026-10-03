package consumers

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/piggystats/rabbitmq"

	"github.com/piggystats/nodera/contexts/notification/aggregates/notification"
	"github.com/piggystats/nodera/contexts/notification/application/handlers"
	"github.com/piggystats/nodera/contexts/notification/async/constants"
	"github.com/piggystats/nodera/contexts/notification/events"
	"github.com/piggystats/nodera/utils/str"
)

//go:generate mockgen -destination=../../mock/consumers/droplet_handler.go -package=mockconsumers github.com/piggystats/nodera/contexts/notification/async/consumers IDropletHandler
type IDropletHandler interface {
	StatusChanged(ctx context.Context, change handlers.DropletStatusChange) error
}

// DropletEvents hands the droplet context's status changes to the handler
// that decides which of them become Notifications.
type DropletEvents struct {
	handler IDropletHandler
}

func NewDropletEvents(handler IDropletHandler) *DropletEvents {
	return &DropletEvents{handler: handler}
}

func (c *DropletEvents) RoutingKeys() []string {
	return []string{constants.DropletStatusChanged}
}

func (c *DropletEvents) Consume(message *rabbitmq.Message) error {
	var event events.DropletStatusChangedEvent
	if err := json.Unmarshal(message.Payload, &event); err != nil {
		return notification.ErrorMalformedEvent.Copy().SetError(err)
	}
	p := event.Payload
	for _, id := range []uuid.UUID{p.DropletID, p.TeamID, p.ProjectID} {
		if id == uuid.Nil {
			return notification.ErrorMalformedEvent.Copy().SetMessage("event payload is missing an id")
		}
	}
	if str.IsBlank(p.Status) || p.ChangedAt.IsZero() {
		return notification.ErrorMalformedEvent.Copy().SetMessage("event payload is missing the status or its time")
	}

	return c.handler.StatusChanged(context.Background(), handlers.DropletStatusChange{
		DropletID: p.DropletID, TeamID: p.TeamID, ProjectID: p.ProjectID,
		Hostname: p.Hostname, IPv4: p.IPv4, From: p.FromStatus, Status: p.Status, FailureCode: p.FailureCode,
	})
}
