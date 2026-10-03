package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/piggystats/nodera/contexts/droplet/application/handlers"
	"github.com/piggystats/nodera/contexts/droplet/async/producers"
	"github.com/piggystats/nodera/contexts/droplet/providers"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/constants"
)

// ReplayBilling writes DropletBillingStarted and DropletBillingStopped for
// every Droplet that was ever active into the outbox. The binary's
// `replay-billing` command and the integration suite both go through it. One
// correlation id ties the whole replay together downstream, since no request
// started it.
func ReplayBilling(ctx context.Context, log logger.ILog, pool *pgxpool.Pool, registry *providers.Registry, outboxClient producers.OutboxClient) (*handlers.ReplaySummary, error) {
	ctx = context.WithValue(ctx, constants.CorrelationID, uuid.NewString())

	services := NewServices(pool)
	replay := handlers.NewReplayBilling(log, services.Droplets, services.Providers, registry, services.Transactions, NewMeter(log, outboxClient), NewStatusAnnouncer(log, outboxClient))

	summary, err := replay.Run(ctx)
	if err != nil {
		return nil, err
	}

	log.Infof("replay-billing queued %d DropletBillingStarted and %d DropletBillingStopped events", summary.Started, summary.Stopped)

	return summary, nil
}
