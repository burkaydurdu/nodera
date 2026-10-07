package main

import (
	"context"

	"github.com/piggystats/nodera/cmd/storage/app"
	"github.com/piggystats/nodera/cmd/storage/config"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/http"
	"github.com/piggystats/nodera/pkg/metrics"
	"github.com/piggystats/nodera/pkg/recover"

	// Swagger output of `make swagger-storage`.
	_ "github.com/piggystats/nodera/contexts/storage/docs"
)

// @title Nodera Storage API
// @version 1.0
// @description Storage inside a Project: Storages on Packages, their Access keys, files and usage. Sizes are bytes; prices are USD excluding taxes.
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

	deps, pool := newDeps(ctx, conf, log, "nodera-storage-server", outboxInstance)
	defer pool.Close()
	handlers := app.NewHandlers(deps)

	management := app.BuildManagement(deps, handlers, http.DefaultRateLimit)
	management.Logger.Fatal(management.Start(conf.Server.Port))
}
