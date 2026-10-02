package main

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"github.com/piggystats/nodera/cmd/billing/app"
	"github.com/piggystats/nodera/cmd/billing/config"
	"github.com/piggystats/nodera/contexts/billing/application/jobs"
	"github.com/piggystats/nodera/contexts/billing/async/consumers"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/metrics"
	"github.com/piggystats/nodera/pkg/recover"
)

const (
	lockExchangeRate = "billing:lock:exchange-rate"
	lockClosePeriods = "billing:lock:close-periods"
	lockThresholds   = "billing:lock:thresholds"
	lockDunning      = "billing:lock:dunning"
	lockReconcile    = "billing:lock:reconcile"
	lockCardExpiring = "billing:lock:card-expiring"
	lockSweep        = "billing:lock:sweep"

	// The rate job stops calling once today is covered, so hourly costs one
	// request an hour only on days without a bulletin yet.
	exchangeRateInterval = time.Hour
	// Period close is idempotent and cheap when nothing is due; a short
	// interval issues the month's Invoices within minutes of midnight UTC.
	closePeriodsInterval = 5 * time.Minute
	// The expiry warning goes once per card, so a daily look is enough.
	cardExpiringInterval = 24 * time.Hour
)

// Consumers never retry here: a failed event goes to the error queue and the
// separate shovel service decides when it comes back. The writes are
// idempotent, so a redelivery after a partial success is harmless.

// RunWorker runs every periodic job under its Redis lock and the event
// consumers in one process.
func RunWorker(conf *config.Config) {
	log := logger.New(conf.Environment)
	ctx := context.Background()
	defer recover.Init(log)

	if conf.Metrics.Enabled {
		metrics.Serve(ctx, log, conf.Metrics.Addr())
	}

	deps := newDependencies(ctx, conf, log, "nodera-billing-worker")
	defer deps.Pool.Close()

	workerJobs := app.BuildJobs(deps)
	locker := jobs.NewLocker(deps.Redis)

	go loop(ctx, log, locker, lockExchangeRate, exchangeRateInterval, workerJobs.FetchExchangeRate)
	go loop(ctx, log, locker, lockClosePeriods, closePeriodsInterval, workerJobs.ClosePeriods)
	go loop(ctx, log, locker, lockThresholds, conf.Jobs.ThresholdInterval(), workerJobs.CheckThresholds)
	go loop(ctx, log, locker, lockDunning, conf.Jobs.DunningInterval(), workerJobs.RunDunning)
	go loop(ctx, log, locker, lockReconcile, conf.Jobs.ReconcileInterval(), workerJobs.ReconcilePayments)
	go loop(ctx, log, locker, lockSweep, conf.Jobs.ReconcileInterval(), workerJobs.SweepInvoices)
	go loop(ctx, log, locker, lockCardExpiring, cardExpiringInterval, workerJobs.CardExpiring)

	rabbitMQClient := newRabbitMQClient(ctx, conf, log)
	consumers.NewWorker(rabbitMQClient, log, app.BuildConsumers(deps)...).Consume(ctx)
	cobra.CheckErr(rabbitMQClient.RunConsumers(ctx))
}

// loop runs the job at once and then on every tick; the lease outlives a
// late tick so a slow run is not taken over, and a crashed replica frees the
// job within it.
func loop(ctx context.Context, log logger.ILog, locker *jobs.Locker, lock string, interval time.Duration, run jobs.Runner) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	ttl := jobs.LockTTL(interval)

	for {
		ran, err := locker.WithLock(ctx, lock, ttl, run)
		if err != nil {
			log.ErrorWithFields(err, logger.NewFields().Add("job", lock))
		} else if !ran {
			log.InfoWithFields("job skipped: another replica holds the lock", logger.NewFields().Add("job", lock))
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
