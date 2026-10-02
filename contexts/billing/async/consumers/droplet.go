package consumers

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/piggystats/rabbitmq"
	"github.com/shopspring/decimal"

	"github.com/piggystats/nodera/contexts/billing/aggregates/usage"
	"github.com/piggystats/nodera/contexts/billing/async/constants"
	"github.com/piggystats/nodera/contexts/billing/async/events"
)

// UsageEvents records a Droplet's billing start and stop as the droplet
// context reports them. The writes are idempotent and order-tolerant: a stop
// that arrives before its start is kept as a tombstone, and a redelivered or
// replayed start never moves the Locked price.
type UsageEvents struct {
	usage usage.IService
}

func NewUsageEvents(usageService usage.IService) *UsageEvents {
	return &UsageEvents{usage: usageService}
}

func (c *UsageEvents) RoutingKeys() []string {
	return []string{constants.DropletBillingStarted, constants.DropletBillingStopped}
}

func (c *UsageEvents) Consume(message *rabbitmq.Message) error {
	ctx := context.Background()

	switch message.RoutingKey {
	case constants.DropletBillingStarted:
		return c.started(ctx, message)
	case constants.DropletBillingStopped:
		return c.stopped(ctx, message)
	default:
		return nil
	}
}

func (c *UsageEvents) started(ctx context.Context, message *rabbitmq.Message) error {
	var event events.DropletBillingStartedEvent
	if err := decodeUsage(message, &event); err != nil {
		return err
	}
	p := event.Payload
	if p.DropletID == uuid.Nil || p.TeamID == uuid.Nil || p.StartedAt.IsZero() {
		return usage.ErrorMalformedEvent.Copy().SetMessage("event payload is missing the droplet, team or start")
	}
	price, err := decimal.NewFromString(p.HourlyPrice)
	if err != nil || price.IsNegative() {
		return usage.ErrorMalformedEvent.Copy().SetMessage("event payload carries no valid hourly price")
	}

	startedAt := p.StartedAt.UTC()

	return c.usage.Start(ctx, &usage.Usage{
		DropletID: p.DropletID, TeamID: p.TeamID, ProjectID: p.ProjectID, Hostname: p.Hostname,
		PlanCode: p.PlanCode, HourlyPrice: price, StartedAt: &startedAt,
	})
}

func (c *UsageEvents) stopped(ctx context.Context, message *rabbitmq.Message) error {
	var event events.DropletBillingStoppedEvent
	if err := decodeUsage(message, &event); err != nil {
		return err
	}
	p := event.Payload
	if p.DropletID == uuid.Nil || p.TeamID == uuid.Nil || p.StoppedAt.IsZero() {
		return usage.ErrorMalformedEvent.Copy().SetMessage("event payload is missing the droplet, team or stop")
	}

	return c.usage.Stop(ctx, p.DropletID, p.TeamID, p.StoppedAt.UTC())
}

// A malformed payload is terminal: redelivering the same bytes cannot fix it.
func decodeUsage(message *rabbitmq.Message, into any) error {
	if err := json.Unmarshal(message.Payload, into); err != nil {
		return usage.ErrorMalformedEvent.Copy().SetError(err)
	}

	return nil
}
