package main

import (
	"context"

	"github.com/piggystats/nodera/cmd/droplet/app"
	"github.com/piggystats/nodera/cmd/droplet/config"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/recover"
)

// RunReplayBilling queues the meter's events for every Droplet that was
// ever active. It only writes the outbox; the `outbox` command relays them.
func RunReplayBilling(ctx context.Context, conf *config.Config) {
	log := logger.New(conf.Environment)
	defer recover.Init(log)

	pool, err := postgresql.Connect(ctx, log, conf.PostgreSQL.ToDBConfig("nodera-droplet-replay"))
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	mongoClient := newMongoClient(conf, log)
	defer func() { _ = mongoClient.GetClient().Disconnect(ctx) }()

	outboxInstance, err := newOutbox(mongoClient, newRabbitMQClient(ctx, conf, log), log)
	if err != nil {
		log.Fatal(err)
	}

	if _, err := app.ReplayBilling(ctx, log, pool, newRegistry(conf, log), outboxInstance); err != nil {
		log.Fatal(err)
	}
}
