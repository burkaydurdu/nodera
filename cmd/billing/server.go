package main

import (
	"context"

	"github.com/piggystats/nodera/cmd/billing/app"
	"github.com/piggystats/nodera/cmd/billing/config"
	"github.com/piggystats/nodera/contexts/billing/aggregates/exchangerate"
	"github.com/piggystats/nodera/contexts/billing/providers"
	"github.com/piggystats/nodera/contexts/billing/providers/craftgate"
	"github.com/piggystats/nodera/contexts/billing/providers/stripe"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/internal/persistence/redis"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/http"
	"github.com/piggystats/nodera/pkg/metrics"
	"github.com/piggystats/nodera/pkg/recover"

	// Swagger output of `make swagger-billing`.
	_ "github.com/piggystats/nodera/contexts/billing/docs"
)

// @title Nodera Billing API
// @version 1.0
// @description Billing profile, payment methods, usage, invoices, payments, billing status and payment provider webhooks of a Team. Money is USD excluding taxes unless a currency says otherwise.
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

	deps := newDependencies(ctx, conf, log, "nodera-billing-server")
	defer deps.Pool.Close()

	server := app.BuildServer(deps, http.DefaultRateLimit)
	server.Logger.Fatal(server.Start(conf.Server.Port))
}

// newDependencies connects what every long-running billing command needs.
func newDependencies(ctx context.Context, conf *config.Config, log logger.ILog, appName string) app.Dependencies {
	pool, err := postgresql.Connect(ctx, log, conf.PostgreSQL.ToDBConfig(appName))
	if err != nil {
		log.Fatal(err)
	}

	return app.Dependencies{
		Log:         log,
		Conf:        conf,
		Pool:        pool,
		Redis:       newRedisService(conf, log),
		Outbox:      newPublishingOutbox(ctx, conf, log),
		Registry:    newRegistry(conf, log),
		RateFetcher: exchangerate.NewTCMBFetcher(conf.Exchange.TCMBURL),
	}
}

func newRedisService(conf *config.Config, log logger.ILog) redis.IService {
	return redis.NewService(redis.NewClient(&redis.Config{
		Host:     conf.Redis.Host,
		Port:     conf.Redis.Port,
		Username: conf.Redis.Username,
		Password: conf.Redis.Password,
		DB:       conf.Redis.DB,
	}, log))
}

// newRegistry builds one adapter per enabled provider block; the routing
// table decides which one a country uses.
func newRegistry(conf *config.Config, log logger.ILog) *providers.Registry {
	registry := providers.NewRegistry()

	if c := conf.Providers.Craftgate; c.Enabled {
		adapter, err := craftgate.New(craftgate.Config{
			APIKey: c.APIKey, SecretKey: c.SecretKey, BaseURL: c.BaseURL, WebhookKey: c.WebhookKey, ThreeDSCallbackKey: c.ThreeDSCallbackKey,
		}, log)
		if err != nil {
			log.Fatal(err)
		}
		registry.Register(adapter)
	}

	if s := conf.Providers.Stripe; s.Enabled {
		adapter, err := stripe.New(stripe.Config{SecretKey: s.SecretKey, WebhookSecret: s.WebhookSecret}, log)
		if err != nil {
			log.Fatal(err)
		}
		registry.Register(adapter)
	}

	return registry
}
