package main

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"github.com/piggystats/nodera/cmd/droplet/app"
	"github.com/piggystats/nodera/cmd/droplet/config"
	"github.com/piggystats/nodera/contexts/droplet/application/jobs"
	"github.com/piggystats/nodera/contexts/droplet/async/consumers"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/metrics"
	"github.com/piggystats/nodera/pkg/recover"
)

const (
	lockSync = "droplet:lock:sync-catalog"
	lockPoll = "droplet:lock:poll-droplets"
)

// Consumers never retry here: a failed event goes to the error queue and the
// separate shovel service decides when it comes back, as for every worker in
// the platform. The upserts are idempotent, so a redelivery after a partial
// success is harmless.

// RunWorker runs the jobs in one process: catalog sync and status poller on
// their intervals under Redis locks, and the customer- and billing-events
// consumers on RabbitMQ. The poller and the billing reactions write the
// meter's events to the outbox; the `outbox` command relays them.
func RunWorker(conf *config.Config) {
	log := logger.New(conf.Environment)
	ctx := context.Background()
	defer recover.Init(log)

	if conf.Metrics.Enabled {
		metrics.Serve(ctx, log, conf.Metrics.Addr())
	}

	pool, err := postgresql.Connect(ctx, log, conf.PostgreSQL.ToDBConfig("nodera-droplet-worker"))
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	redisService := newRedisService(conf, log)
	registry := newRegistry(conf, log)
	rabbitMQClient := newRabbitMQClient(ctx, conf, log)

	mongoClient := newMongoClient(conf, log)
	defer func() { _ = mongoClient.GetClient().Disconnect(ctx) }()
	outboxInstance, err := newOutbox(mongoClient, rabbitMQClient, log)
	if err != nil {
		log.Fatal(err)
	}

	workerJobs := app.BuildJobs(log, pool, registry, outboxInstance)
	locker := jobs.NewLocker(redisService)

	go loop(ctx, log, locker, lockSync, conf.Jobs.SyncInterval(), workerJobs.SyncCatalog)
	go loop(ctx, log, locker, lockPoll, conf.Jobs.PollInterval(), workerJobs.PollDroplets)

	consumers.NewWorker(rabbitMQClient, log, app.BuildCustomerConsumer(pool), app.BuildBillingConsumer(log, pool, registry, outboxInstance)).Consume(ctx)
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
