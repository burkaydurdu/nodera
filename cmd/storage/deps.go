package main

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/piggystats/nodera/cmd/storage/app"
	"github.com/piggystats/nodera/cmd/storage/config"
	"github.com/piggystats/nodera/contexts/storage/async/producers"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/internal/persistence/redis"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/secrets"
)

// newDeps builds what every storage process runs over; the caller closes the
// pool.
func newDeps(ctx context.Context, conf *config.Config, log logger.ILog, appName string, outbox producers.OutboxClient) (app.Deps, *pgxpool.Pool) {
	pool, err := postgresql.Connect(ctx, log, conf.PostgreSQL.ToDBConfig(appName))
	if err != nil {
		log.Fatal(err)
	}

	sealer, err := secrets.NewSealerFromBase64(conf.Secret.EncryptionKey)
	if err != nil {
		log.Fatal(err)
	}
	eng, err := newEngine(conf)
	if err != nil {
		log.Fatal(err)
	}

	return app.Deps{
		Log:    log,
		Conf:   conf,
		Pool:   pool,
		Redis:  newRedisService(conf, log),
		Outbox: outbox,
		Engine: eng,
		Sealer: sealer,
		Clock:  time.Now,
	}, pool
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
