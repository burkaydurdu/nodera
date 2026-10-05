package main

import (
	"context"

	"github.com/piggystats/nodera/cmd/customer/app"
	"github.com/piggystats/nodera/cmd/customer/config"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/recover"
)

// RunSyncAPIKeys puts Redis back in line with the database's API keys, for
// when Redis was lost or drifted (docs/adr/0010).
func RunSyncAPIKeys(ctx context.Context, conf *config.Config) {
	log := logger.New(conf.Environment)
	defer recover.Init(log)

	pool, err := postgresql.Connect(ctx, log, conf.PostgreSQL.ToDBConfig("nodera-customer-sync-api-keys"))
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	if _, err := app.SyncAPIKeys(ctx, log, pool, newRedisService(conf, log)); err != nil {
		log.Fatal(err)
	}
}
