package main

import (
	"context"
	"time"

	"github.com/piggystats/outbox"
	"github.com/piggystats/rabbitmq"

	"github.com/piggystats/nodera/cmd/customer/config"
	"github.com/piggystats/nodera/contexts/customer/async/producers"
	"github.com/piggystats/nodera/internal/persistence/mongo"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/metrics"
	"github.com/piggystats/nodera/pkg/recover"
	"github.com/piggystats/nodera/utils/convert"
)

const (
	outboxWorkers   = 5
	outboxBatchSize = 50
	// The relay polls Mongo; the library's ten seconds would make a live
	// update in the web app late by that much (docs/adr/0007).
	outboxInterval = time.Second
	prefetchCount  = 32
)

func newRabbitMQClient(ctx context.Context, conf *config.Config, log logger.ILog) rabbitmq.IClient {
	return rabbitmq.NewRabbitMqClient(
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
}

func newMongoClient(conf *config.Config, log logger.ILog) mongo.IClient {
	return mongo.NewClient(&mongo.Config{
		Host:     conf.MongoDB.Host,
		Port:     conf.MongoDB.Port,
		Username: conf.MongoDB.Username,
		Password: conf.MongoDB.Password,
		Database: conf.MongoDB.Database,
		URI:      conf.MongoDB.URI,
	}, log)
}

func newOutbox(mongoClient mongo.IClient, publisher outbox.Publisher, log logger.ILog) (*outbox.Outbox, error) {
	return outbox.New(
		mongoClient.GetClient(),
		mongoClient.GetDatabase(),
		publisher,
		log,
		rabbitmq.CorrelationID,
		outbox.WithWorkers(outboxWorkers),
		outbox.WithBatchSize(outboxBatchSize),
		outbox.WithRetryInterval(outboxInterval),
	)
}

// RunOutbox relays stored messages to RabbitMQ and returns the shutdown hook.
func RunOutbox(ctx context.Context, conf *config.Config) func(context.Context) error {
	log := logger.New(conf.Environment)
	defer recover.Init(log)

	if conf.Metrics.Enabled {
		metrics.Serve(ctx, log, conf.Metrics.Addr())
	}

	rabbitMQClient := newRabbitMQClient(ctx, conf, log)
	producers.NewProducer(rabbitMQClient).AddPublisher(ctx)

	mongoClient := newMongoClient(conf, log)
	outboxInstance, err := newOutbox(mongoClient, rabbitMQClient, log)
	if err != nil {
		log.Fatal(err)
	}

	go outboxInstance.StartProcessor()

	return func(shutdownCtx context.Context) error {
		log.Info("Stopping outbox processor...")
		outboxInstance.Stop()

		return mongoClient.GetClient().Disconnect(shutdownCtx)
	}
}
