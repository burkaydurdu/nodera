package main

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"github.com/piggystats/nodera/cmd/storage/app"
	"github.com/piggystats/nodera/cmd/storage/config"
	"github.com/piggystats/nodera/contexts/storage/application/jobs"
	"github.com/piggystats/nodera/contexts/storage/async/consumers"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/joblock"
	"github.com/piggystats/nodera/pkg/metrics"
	"github.com/piggystats/nodera/pkg/recover"
)

// Consumers never retry here: a failed event goes to the error queue and the
// separate shovel service decides when it comes back, as for every worker in
// the platform. The read-model writes are idempotent, so a redelivery after a
// partial success is harmless.

// RunWorker runs every job on its own interval under its own Redis lock, and
// the customer- and billing-events consumers.
func RunWorker(conf *config.Config) {
	log := logger.New(conf.Environment)
	ctx := context.Background()
	defer recover.Init(log)

	if conf.Metrics.Enabled {
		metrics.Serve(ctx, log, conf.Metrics.Addr())
	}

	rabbitMQClient := newRabbitMQClient(ctx, conf, log)
	mongoClient := newMongoClient(conf, log)
	defer func() { _ = mongoClient.GetClient().Disconnect(ctx) }()
	outboxInstance, err := newOutbox(mongoClient, rabbitMQClient, log)
	if err != nil {
		log.Fatal(err)
	}

	deps, pool := newDeps(ctx, conf, log, "nodera-storage-worker", outboxInstance)
	defer pool.Close()
	handlers := app.NewHandlers(deps)
	workerJobs := app.BuildJobs(deps, handlers)
	locker := joblock.NewLocker(deps.Redis)

	for _, job := range []struct {
		lock     string
		interval time.Duration
		run      jobs.Runner
	}{
		{"storage:lock:measure", conf.Jobs.Measure(), workerJobs.Measure},
		{"storage:lock:traffic", conf.Jobs.Traffic(), workerJobs.Traffic},
		{"storage:lock:roll-periods", conf.Jobs.Rollover(), workerJobs.RollPeriods},
		{"storage:lock:purge", conf.Jobs.Purge(), workerJobs.Purge},
		{"storage:lock:reconcile", conf.Jobs.Reconcile(), workerJobs.Reconcile},
	} {
		go loop(ctx, log, locker, job.lock, job.interval, job.run)
	}

	consumers.NewWorker(rabbitMQClient, log, app.BuildCustomerConsumer(handlers), app.BuildBillingConsumer(handlers)).Consume(ctx)
	cobra.CheckErr(rabbitMQClient.RunConsumers(ctx))
}

// loop runs the job at once and then on every tick; the lease outlives a
// late tick so a slow run is not taken over, and a crashed replica frees the
// job within it.
func loop(ctx context.Context, log logger.ILog, locker *joblock.Locker, lock string, interval time.Duration, run jobs.Runner) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	ttl := joblock.LockTTL(interval)

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
