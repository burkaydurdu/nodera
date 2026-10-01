package main

import (
	"context"
	"time"

	"github.com/piggystats/rabbitmq"
	"github.com/spf13/cobra"

	"github.com/piggystats/nodera/cmd/droplet/app"
	"github.com/piggystats/nodera/cmd/droplet/config"
	"github.com/piggystats/nodera/contexts/droplet/application/jobs"
	"github.com/piggystats/nodera/contexts/droplet/async/consumers"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/metrics"
	"github.com/piggystats/nodera/pkg/recover"
	"github.com/piggystats/nodera/utils/convert"
)

const (
	prefetchCount = 32
	lockSync      = "droplet:lock:sync-catalog"
	lockPoll      = "droplet:lock:poll-droplets"
)

// Consumers never retry here: a failed event goes to the error queue and the
// separate shovel service decides when it comes back, as for every worker in
// the platform. The upserts are idempotent, so a redelivery after a partial
// success is harmless.

// RunWorker runs the three jobs in one process: catalog sync and status
// poller on their intervals under Redis locks, and the customer-events
// consumer on RabbitMQ.
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
	workerJobs := app.BuildJobs(log, pool, registry)
	locker := jobs.NewLocker(redisService)

	go loop(ctx, log, locker, lockSync, conf.Jobs.SyncInterval(), workerJobs.SyncCatalog)
	go loop(ctx, log, locker, lockPoll, conf.Jobs.PollInterval(), workerJobs.PollDroplets)

	rabbitMQClient := rabbitmq.NewRabbitMqClient(
		ctx,
		[]string{conf.RabbitMQ.Host},
		conf.RabbitMQ.Username,
		conf.RabbitMQ.Password,
		conf.RabbitMQ.VirtualHost,
		log,
		rabbitmq.RetryCount(0),
		rabbitmq.PrefetchCount(prefetchCount),
		rabbitmq.WithMessageAuthentication(convert.StringToBytes(conf.RabbitMQ.AuthSecret), rabbitmq.AuthModeStrict),
	)

	consumers.NewWorker(rabbitMQClient, log, app.BuildCustomerConsumer(pool)).Consume(ctx)
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
