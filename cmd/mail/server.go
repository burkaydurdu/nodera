package main

import (
	"context"

	"github.com/piggystats/nodera/cmd/mail/app"
	"github.com/piggystats/nodera/cmd/mail/config"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/http"
	"github.com/piggystats/nodera/pkg/metrics"
	"github.com/piggystats/nodera/pkg/recover"

	// Swagger output of `make swagger-mail`.
	_ "github.com/piggystats/nodera/contexts/mail/docs"
)

// @title Nodera Mail API
// @version 1.0
// @description Mail inside a Project: Packages, Sending domains, logs, Suppressions and Webhooks, and the sending API software reaches with an API key. Prices are USD excluding taxes.
// @basePath /
// @securityDefinitions.apiKey BearerAuth
// @in header
// @name Authorization
func RunServer(conf *config.Config) {
	log := logger.New(conf.Environment)
	ctx := context.Background()
	defer recover.Init(log)

	if conf.Metrics.Enabled {
		metrics.Serve(ctx, log, conf.Metrics.Addr())
	}

	// The server writes the outbox; the `outbox` command relays it.
	mongoClient := newMongoClient(conf, log)
	defer func() { _ = mongoClient.GetClient().Disconnect(ctx) }()
	outboxInstance, err := newOutbox(mongoClient, newRabbitMQClient(ctx, conf, log), log)
	if err != nil {
		log.Fatal(err)
	}

	deps, pool := newDeps(ctx, conf, log, "nodera-mail-server", outboxInstance)
	defer pool.Close()
	handlers := app.NewHandlers(deps)

	sending := app.BuildSending(deps, handlers)
	internal := app.BuildInternal(deps, handlers)
	management := app.BuildManagement(deps, handlers, http.DefaultRateLimit)

	go func() { sending.Logger.Fatal(sending.Start(conf.Server.SendingPort)) }()
	go func() { internal.Logger.Fatal(internal.Start(conf.Server.InternalPort)) }()
	management.Logger.Fatal(management.Start(conf.Server.Port))
}
