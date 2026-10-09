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

//go:generate mockgen -destination=../../mock/consumers/cluster_handler.go -package=mockconsumers github.com/piggystats/nodera/contexts/notification/async/consumers IClusterHandler
type IClusterHandler interface {
	StatusChanged(ctx context.Context, change handlers.ClusterStatusChange) error
}

// ClusterEvents hands the kubernetes context's status changes to the handler
// that decides which of them become Notifications.
type ClusterEvents struct {
	handler IClusterHandler
}

func NewClusterEvents(handler IClusterHandler) *ClusterEvents {
	return &ClusterEvents{handler: handler}
}

func (c *ClusterEvents) RoutingKeys() []string {
	return []string{constants.ClusterStatusChanged}
}

func (c *ClusterEvents) Consume(message *rabbitmq.Message) error {
	var event events.ClusterStatusChangedEvent
	if err := json.Unmarshal(message.Payload, &event); err != nil {
		return notification.ErrorMalformedEvent.Copy().SetError(err)
	}
	p := event.Payload
	for _, id := range []uuid.UUID{p.ClusterID, p.TeamID, p.ProjectID} {
		if id == uuid.Nil {
			return notification.ErrorMalformedEvent.Copy().SetMessage("event payload is missing an id")
		}
	}
	if str.IsBlank(p.Status) || p.ChangedAt.IsZero() {
		return notification.ErrorMalformedEvent.Copy().SetMessage("event payload is missing the status or its time")
	}

	return c.handler.StatusChanged(context.Background(), handlers.ClusterStatusChange{
		ClusterID: p.ClusterID, TeamID: p.TeamID, ProjectID: p.ProjectID, ProjectName: p.ProjectName, Name: p.Name,
		From: p.FromStatus, Status: p.Status, FailureCode: p.FailureCode,
	})
}
