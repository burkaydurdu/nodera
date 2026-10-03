package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/piggystats/rabbitmq"

	"github.com/piggystats/nodera/cmd/realtime/app"
	"github.com/piggystats/nodera/cmd/realtime/config"
	"github.com/piggystats/nodera/contexts/realtime/consumers"
	"github.com/piggystats/nodera/internal/persistence/redis"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/metrics"
	"github.com/piggystats/nodera/pkg/realtime"
	"github.com/piggystats/nodera/pkg/recover"
	"github.com/piggystats/nodera/utils/convert"
)

const (
	prefetchCount   = 32
	shutdownTimeout = 30 * time.Second
)

// RunServer serves the sockets and consumes the realtime queue in one
// process. The consumer never retries: a message that fails goes to the error
// queue like any other, and a late nudge is harmless.
func RunServer(conf *config.Config) {
	log := logger.New(conf.Environment)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer recover.Init(log)

	if conf.Metrics.Enabled {
		metrics.Serve(ctx, log, conf.Metrics.Addr())
	}

	replica := app.Build(app.Dependencies{
		Log: log,
		Redis: redis.NewClient(&redis.Config{
			Host: conf.Redis.Host, Port: conf.Redis.Port, Username: conf.Redis.Username, Password: conf.Redis.Password, DB: conf.Redis.DB,
		}, log),
		Settings: app.Settings{
			JWTSecret:      conf.Secret.JWT,
			OriginPatterns: conf.WebSocket.OriginPatterns,
			AuthTimeout:    conf.WebSocket.AuthTimeout(),
			Heartbeat:      conf.WebSocket.Heartbeat(),
		},
	})
	if err := replica.Start(ctx); err != nil {
		log.Fatal(err)
	}

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
	rabbitMQClient.
		AddConsumer(consumers.QueuePrefix+realtime.RoutingKey).
		SubscriberExchange(realtime.RoutingKey, rabbitmq.Topic, consumers.CommonExchange).
		HandleConsumer(replica.Consumer.Consume)
	go func() {
		if err := rabbitMQClient.RunConsumers(ctx); err != nil {
			log.Fatal(err)
		}
	}()

	go func() {
		if err := replica.Server.Start(conf.Server.Port); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	<-signals

	// Stop taking sockets first, so a reconnect lands on another replica,
	// then tell the open ones to go.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()
	if err := replica.Server.Shutdown(shutdownCtx); err != nil {
		log.Error(err)
	}
	log.Info("shutting down: closing the sockets")
	if err := replica.Shutdown(shutdownCtx); err != nil {
		log.Error(err)
	}
}
