package main

import (
	"context"

	"github.com/piggystats/nodera/cmd/droplet/app"
	"github.com/piggystats/nodera/cmd/droplet/config"
	"github.com/piggystats/nodera/contexts/droplet/providers"
	"github.com/piggystats/nodera/contexts/droplet/providers/cherry"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/internal/persistence/redis"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/http"
	"github.com/piggystats/nodera/pkg/metrics"
	"github.com/piggystats/nodera/pkg/recover"

	// Swagger output of `make swagger-droplet`.
	_ "github.com/piggystats/nodera/contexts/droplet/docs"
)

// @title Nodera Droplet API
// @version 1.0
// @description Catalog (Regions, Plans, Images) and Droplets inside a Project. Prices are USD excluding taxes.
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

	pool, err := postgresql.Connect(ctx, log, conf.PostgreSQL.ToDBConfig("nodera-droplet-server"))
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	redisService := newRedisService(conf, log)
	registry := newRegistry(conf, log)

	server := app.BuildServer(log, conf, pool, redisService, registry, http.DefaultRateLimit)
	server.Logger.Fatal(server.Start(conf.Server.Port))
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

// newRegistry builds one adapter per enabled Provider block in the config.
func newRegistry(conf *config.Config, log logger.ILog) *providers.Registry {
	registry := providers.NewRegistry()

	if conf.Providers.Cherry.Enabled {
		adapter, err := cherry.New(cherry.Config{
			Token:     conf.Providers.Cherry.Token,
			ProjectID: conf.Providers.Cherry.ProjectID,
			Endpoint:  conf.Providers.Cherry.Endpoint,
		}, log)
		if err != nil {
			log.Fatal(err)
		}
		registry.Register(adapter)
	}

	return registry
}
