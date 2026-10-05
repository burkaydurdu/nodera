package app

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/piggystats/nodera/contexts/customer/aggregates/apikey"
	"github.com/piggystats/nodera/contexts/customer/aggregates/apikey/entry"
	"github.com/piggystats/nodera/contexts/customer/application/handlers"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/internal/persistence/redis"
	"github.com/piggystats/nodera/logger"
)

// SyncAPIKeys rewrites the Redis entry of every active API key and deletes
// those of revoked keys, through the same store the server writes with. The
// binary's `sync-api-keys` command and the integration suite both go through
// it.
func SyncAPIKeys(ctx context.Context, log logger.ILog, pool *pgxpool.Pool, redisService redis.IService) (*handlers.APIKeySyncSummary, error) {
	sync := handlers.NewAPIKeySync(
		apikey.NewService(apikey.NewRepository(postgresql.New(pool))),
		entry.NewStore(redisService),
	)

	summary, err := sync.Run(ctx)
	if err != nil {
		return nil, err
	}

	log.Infof("sync-api-keys wrote %d active keys and deleted %d revoked ones", summary.Written, summary.Deleted)

	return summary, nil
}
