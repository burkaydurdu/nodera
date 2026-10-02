package handlers

import (
	"context"
	"time"

	"github.com/piggystats/nodera/contexts/droplet/aggregates/droplet"
	"github.com/piggystats/nodera/logger"
)

//go:generate mockgen -destination=../../mock/producers/billing_started.go -package=mockproducers github.com/piggystats/nodera/contexts/droplet/application/handlers IBillingStartedProducer
type IBillingStartedProducer interface {
	Publish(ctx context.Context, d *droplet.Droplet, startedAt time.Time) error
}

//go:generate mockgen -destination=../../mock/producers/billing_stopped.go -package=mockproducers github.com/piggystats/nodera/contexts/droplet/application/handlers IBillingStoppedProducer
type IBillingStoppedProducer interface {
	Publish(ctx context.Context, d *droplet.Droplet, stoppedAt time.Time) error
}

// Meter announces the usage boundaries a Droplet crossed to the billing
// context. It runs after the commit that recorded the claim (docs/adr/0001),
// so a failed publish is logged rather than turned into an error the caller
// cannot act on; `replay-billing` is the recovery path.
type Meter struct {
	log     logger.ILog
	started IBillingStartedProducer
	stopped IBillingStoppedProducer
}

func NewMeter(log logger.ILog, started IBillingStartedProducer, stopped IBillingStoppedProducer) *Meter {
	return &Meter{log: log, started: started, stopped: stopped}
}

func (m *Meter) Publish(ctx context.Context, d *droplet.Droplet, events []droplet.BillingEvent) {
	for _, event := range events {
		var err error
		switch event {
		case droplet.BillingStarted:
			err = m.started.Publish(ctx, d, *d.BillingStartedAt())
		case droplet.BillingStopped:
			err = m.stopped.Publish(ctx, d, *d.BillingStoppedAt())
		}
		if err != nil {
			m.log.ErrorWithFields(err, logger.NewFields().Add("droplet", d.ID.String()).Add("billing_event", string(event)))
		}
	}
}
