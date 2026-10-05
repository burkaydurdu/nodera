package main

import (
	"context"

	"github.com/piggystats/nodera/cmd/customer/app"
	"github.com/piggystats/nodera/cmd/customer/config"
	"github.com/piggystats/nodera/contexts/customer/async/producers"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/internal/persistence/redis"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/http"
	"github.com/piggystats/nodera/pkg/metrics"
	"github.com/piggystats/nodera/pkg/recover"

	// Swagger output of `make swagger-customer`.
	_ "github.com/piggystats/nodera/contexts/customer/docs"
)

// @title Nodera Customer API
// @version 1.0
// @description Customer-facing API of Nodera: sign-up, sign-in, profile, teams, members, invitations and projects.
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

	pool, err := postgresql.Connect(ctx, log, conf.PostgreSQL.ToDBConfig("nodera-customer-server"))
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	redisService := newRedisService(conf, log)

	rabbitMQClient := newRabbitMQClient(ctx, conf, log)
	producers.NewProducer(rabbitMQClient).AddPublisher(ctx)

	mongoClient := newMongoClient(conf, log)
	outboxInstance, err := newOutbox(mongoClient, rabbitMQClient, log)
	if err != nil {
		log.Fatal(err)
	}

	server := app.BuildServer(log, conf, pool, redisService, outboxInstance, http.DefaultRateLimit)
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
