package main

import (
	"context"

	"github.com/piggystats/nodera/cmd/customer/app"
	"github.com/piggystats/nodera/cmd/customer/config"
	"github.com/piggystats/nodera/contexts/customer/async/producers"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/recover"
)

// RunReplayProjects queues the Team and Project events for every existing
// row. It only writes the outbox; the `outbox` command relays them.
func RunReplayProjects(ctx context.Context, conf *config.Config) {
	log := logger.New(conf.Environment)
	defer recover.Init(log)

	pool, err := postgresql.Connect(ctx, log, conf.PostgreSQL.ToDBConfig("nodera-customer-replay"))
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	rabbitMQClient := newRabbitMQClient(ctx, conf, log)
	producers.NewProducer(rabbitMQClient).AddPublisher(ctx)

	mongoClient := newMongoClient(conf, log)
	defer func() { _ = mongoClient.GetClient().Disconnect(ctx) }()

	outboxInstance, err := newOutbox(mongoClient, rabbitMQClient, log)
	if err != nil {
		log.Fatal(err)
	}

	if _, err := app.ReplayProjects(ctx, log, pool, outboxInstance); err != nil {
		log.Fatal(err)
	}
}
