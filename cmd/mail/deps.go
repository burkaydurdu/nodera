package main

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/piggystats/nodera/cmd/mail/app"
	"github.com/piggystats/nodera/cmd/mail/config"
	"github.com/piggystats/nodera/contexts/mail/aggregates/message"
	"github.com/piggystats/nodera/contexts/mail/async/producers"
	"github.com/piggystats/nodera/contexts/mail/mta/kumomta"
	"github.com/piggystats/nodera/contexts/mail/secrets"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/internal/persistence/redis"
	"github.com/piggystats/nodera/logger"
)

// returnPathPurpose draws the key that tags return paths from the configured
// encryption key; changing it orphans the bounces of mail already sent.
const returnPathPurpose = "nodera mail return paths"

// newDeps builds what every mail process runs over; the caller closes the
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
	returnPathKey, err := secrets.DeriveFromBase64(conf.Secret.EncryptionKey, returnPathPurpose)
	if err != nil {
		log.Fatal(err)
	}

	return app.Deps{
		Log:    log,
		Conf:   conf,
		Pool:   pool,
		Redis:  newRedisService(conf, log),
		Outbox: outbox,
		MTA: kumomta.New(kumomta.Config{
			InjectionURL: conf.MTA.InjectionURL, Username: conf.MTA.Username, Password: conf.MTA.Password,
			Timeout: time.Duration(conf.MTA.TimeoutSeconds) * time.Second,
		}),
		Resolver:    app.NewResolver(conf.DNS.Resolver),
		Sealer:      sealer,
		ReturnPaths: message.NewReturnPaths(returnPathKey),
		Clock:       time.Now,
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
