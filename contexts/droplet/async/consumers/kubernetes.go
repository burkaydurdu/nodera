package consumers

import (
	"context"

	"github.com/google/uuid"
	"github.com/piggystats/rabbitmq"

	"github.com/piggystats/nodera/contexts/droplet/aggregates/access"
	"github.com/piggystats/nodera/contexts/droplet/application/handlers"
	"github.com/piggystats/nodera/contexts/droplet/async/constants"
	"github.com/piggystats/nodera/contexts/droplet/async/events"
	contextConstants "github.com/piggystats/nodera/pkg/constants"
	"github.com/piggystats/nodera/utils/str"
)

// NodesHandler is the slice of the application the kubernetes-events consumer
// delegates to; it binds and validates, the handler decides.
//
//go:generate mockgen -destination=../../mock/consumers/nodes_handler.go -package=mockconsumers github.com/piggystats/nodera/contexts/droplet/async/consumers NodesHandler
type NodesHandler interface {
	Request(ctx context.Context, order handlers.NodeOrder) error
	Release(ctx context.Context, nodeID uuid.UUID) error
}

// KubernetesEvents opens and releases Node Droplets at the kubernetes
// context's request (docs/adr/0014).
type KubernetesEvents struct {
	handler NodesHandler
}

func NewKubernetesEvents(handler NodesHandler) *KubernetesEvents {
	return &KubernetesEvents{handler: handler}
}

// Consume dispatches on the routing key; a key this context does not consume
// is acknowledged and ignored. The message's correlation id rides the
// context so the events this reaction publishes stay traceable to it.
func (c *KubernetesEvents) Consume(message *rabbitmq.Message) error {
	ctx := context.Background()
	if correlationID := message.GetCorrelationID(); str.IsNotEmpty(correlationID) {
		ctx = context.WithValue(ctx, contextConstants.CorrelationID, correlationID)
	}

	switch message.RoutingKey {
	case constants.NodeRequested:
		var event events.NodeRequestedEvent
		if err := decode(message, &event); err != nil {
			return err
		}
		p := event.Payload
		if err := requireIDs(p.NodeID, p.ClusterID, p.ProjectID, p.TeamID); err != nil {
			return err
		}
		if str.IsBlank(p.Plan) || str.IsBlank(p.Region) || str.IsBlank(p.Image) || str.IsBlank(p.Hostname) || str.IsBlank(p.Pool) {
			return access.ErrorMalformedEvent.Copy().SetMessage("event payload is missing the catalog codes, the hostname or the pool")
		}

		return c.handler.Request(ctx, handlers.NodeOrder{
			NodeID: p.NodeID, ClusterID: p.ClusterID, Pool: p.Pool, ProjectID: p.ProjectID, TeamID: p.TeamID,
			Plan: p.Plan, Region: p.Region, Image: p.Image, Hostname: p.Hostname,
		})

	case constants.NodeReleased:
		var event events.NodeReleasedEvent
		if err := decode(message, &event); err != nil {
			return err
		}
		if err := requireIDs(event.Payload.NodeID); err != nil {
			return err
		}

		return c.handler.Release(ctx, event.Payload.NodeID)

	default:
		return nil
	}
}
