package consumers

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/piggystats/rabbitmq"

	"github.com/piggystats/nodera/contexts/droplet/aggregates/billing"
	"github.com/piggystats/nodera/contexts/droplet/async/constants"
	"github.com/piggystats/nodera/contexts/droplet/async/events"
	contextConstants "github.com/piggystats/nodera/pkg/constants"
	"github.com/piggystats/nodera/utils/str"
)

// TeamBillingHandler is the slice of the application the billing-events
// consumer delegates to; it binds and validates, the handler decides.
type TeamBillingHandler interface {
	StatusChanged(ctx context.Context, teamID uuid.UUID, status billing.Status, changedAt time.Time) error
	DeletionRequested(ctx context.Context, teamID uuid.UUID) error
}

// BillingEvents applies the billing context's events (docs/adr/0006).
type BillingEvents struct {
	handler TeamBillingHandler
}

func NewBillingEvents(handler TeamBillingHandler) *BillingEvents {
	return &BillingEvents{handler: handler}
}

// Consume dispatches on the routing key; a key this context does not consume
// is acknowledged and ignored. The message's correlation id rides the
// context so the events this reaction publishes stay traceable to it.
func (c *BillingEvents) Consume(message *rabbitmq.Message) error {
	ctx := context.Background()
	if correlationID := message.GetCorrelationID(); str.IsNotEmpty(correlationID) {
		ctx = context.WithValue(ctx, contextConstants.CorrelationID, correlationID)
	}

	switch message.RoutingKey {
	case constants.TeamBillingStatusChanged:
		var event events.TeamBillingStatusChangedEvent
		if err := decodeBilling(message, &event); err != nil {
			return err
		}
		if event.Payload.TeamID == uuid.Nil {
			return billing.ErrorMalformedEvent.Copy().SetMessage("event payload is missing the team id")
		}
		if event.Payload.ChangedAt.IsZero() {
			return billing.ErrorMalformedEvent.Copy().SetMessage("event payload is missing its time")
		}
		status := billing.Status(event.Payload.Status)
		if !status.IsValid() {
			return billing.ErrorMalformedEvent.Copy().SetMessage("event payload carries an unknown status")
		}

		return c.handler.StatusChanged(ctx, event.Payload.TeamID, status, event.Payload.ChangedAt)

	case constants.TeamDropletsDeletionRequested:
		var event events.TeamDropletsDeletionRequestedEvent
		if err := decodeBilling(message, &event); err != nil {
			return err
		}
		if event.Payload.TeamID == uuid.Nil {
			return billing.ErrorMalformedEvent.Copy().SetMessage("event payload is missing the team id")
		}

		return c.handler.DeletionRequested(ctx, event.Payload.TeamID)

	default:
		return nil
	}
}
