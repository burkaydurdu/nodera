package main

import (
	"context"

	"github.com/piggystats/nodera/cmd/notification/app"
	"github.com/piggystats/nodera/cmd/notification/config"
	"github.com/piggystats/nodera/contexts/notification/async/producers"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/internal/persistence/redis"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/http"
	"github.com/piggystats/nodera/pkg/metrics"
	"github.com/piggystats/nodera/pkg/recover"

	// Swagger output of `make swagger-notification`.
	_ "github.com/piggystats/nodera/contexts/notification/docs"
)

// @title Nodera Notification API
// @version 1.0
// @description The signed-in User's Inbox: the In-app Notifications of every Team they are in.
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

	pool, err := postgresql.Connect(ctx, log, conf.PostgreSQL.ToDBConfig("nodera-notification-server"))
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	// The server publishes only: a read tells the User's other tabs.
	publisher := producers.NewRealtime(newRabbitMQClient(ctx, conf, log))
	publisher.AddPublisher(ctx)

	server := app.BuildServer(app.Dependencies{
		Log: log, Conf: conf, Pool: pool, Redis: newRedisService(conf, log), Realtime: publisher,
	}, http.DefaultRateLimit)
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
