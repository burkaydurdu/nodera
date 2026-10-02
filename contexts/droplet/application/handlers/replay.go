package handlers

import (
	"context"

	"github.com/piggystats/nodera/contexts/droplet/aggregates/droplet"
	"github.com/piggystats/nodera/contexts/droplet/aggregates/provider"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/logger"
)

type ReplaySummary struct {
	Started int
	Stopped int
}

// ReplayBilling republishes the meter's boundaries for every Droplet that
// was ever active, so a billing context built after the Droplets, or one
// that lost events, can catch up. The billing context keeps one usage row
// per Droplet, so running it twice is harmless.
type ReplayBilling struct {
	*dropletOps
}

func NewReplayBilling(
	log logger.ILog,
	droplets droplet.IService,
	providerService provider.IService,
	registry ProviderRegistry,
	transactions postgresql.ITransactionManager,
	meter *Meter,
) *ReplayBilling {
	return &ReplayBilling{
		dropletOps: &dropletOps{log: log, droplets: droplets, providers: providerService, registry: registry, transactions: transactions, meter: meter},
	}
}

// Run publishes Started, then Stopped where the meter has stopped, for each
// billed Droplet, and records both as announced so the regular path does not
// announce them again later.
func (h *ReplayBilling) Run(ctx context.Context) (*ReplaySummary, error) {
	billed, err := h.droplets.FindBilled(ctx)
	if err != nil {
		return nil, err
	}

	summary := &ReplaySummary{}
	for _, d := range billed {
		var (
			current *droplet.Droplet
			events  []droplet.BillingEvent
		)
		err := h.update(ctx, d.ID, func(ctx context.Context, locked *droplet.Droplet) error {
			current = locked
			events = locked.BillingEvents()
			locked.MarkBillingPublished(droplet.Now())

			return h.droplets.Save(ctx, locked)
		})
		if err != nil {
			return nil, err
		}

		h.meter.Publish(ctx, current, events)
		for _, event := range events {
			switch event {
			case droplet.BillingStarted:
				summary.Started++
			case droplet.BillingStopped:
				summary.Stopped++
			}
		}
	}

	return summary, nil
}
