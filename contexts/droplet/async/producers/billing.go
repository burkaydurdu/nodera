package producers

import (
	"context"
	"time"

	"github.com/piggystats/nodera/contexts/droplet/aggregates/catalog"
	"github.com/piggystats/nodera/contexts/droplet/aggregates/droplet"
	"github.com/piggystats/nodera/contexts/droplet/async/constants"
	"github.com/piggystats/nodera/contexts/droplet/async/events"
)

// Timestamps go out in UTC so every consumer reads the same instant whatever
// zone the database handed back.

type BillingStarted struct{ client OutboxClient }

func NewBillingStarted(client OutboxClient) *BillingStarted {
	return &BillingStarted{client: client}
}

func (p *BillingStarted) Publish(ctx context.Context, d *droplet.Droplet, startedAt time.Time) error {
	return publish(ctx, p.client, constants.DropletBillingStarted, &events.DropletBillingStartedEvent{
		Payload: events.DropletBillingStartedBody{
			DropletID:   d.ID,
			TeamID:      d.TeamID,
			ProjectID:   d.ProjectID,
			Hostname:    d.Hostname,
			PlanCode:    d.PlanCode,
			HourlyPrice: d.HourlyPrice.StringFixed(catalog.HourlyScale),
			StartedAt:   startedAt.UTC(),
		},
	})
}

type BillingStopped struct{ client OutboxClient }

func NewBillingStopped(client OutboxClient) *BillingStopped {
	return &BillingStopped{client: client}
}

func (p *BillingStopped) Publish(ctx context.Context, d *droplet.Droplet, stoppedAt time.Time) error {
	return publish(ctx, p.client, constants.DropletBillingStopped, &events.DropletBillingStoppedEvent{
		Payload: events.DropletBillingStoppedBody{DropletID: d.ID, TeamID: d.TeamID, StoppedAt: stoppedAt.UTC()},
	})
}
