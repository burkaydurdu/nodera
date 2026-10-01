package postgresql

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/piggystats/nodera/logger"
)

const (
	defaultMaxConns          int32         = 4
	defaultMaxConnIdleTime   time.Duration = 2 * time.Minute
	defaultMaxConnLifetime   time.Duration = 30 * time.Minute
	defaultHealthCheckPeriod time.Duration = 1 * time.Minute
)

type DBTX interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// Queries holds the pool every repository is built on.
type Queries struct {
	DB DBTX
}

func New(db DBTX) *Queries {
	return &Queries{DB: db}
}

// Executor answers the transaction riding on ctx, or the pool.
func (q *Queries) Executor(ctx context.Context) DBTX {
	if tx, ok := TxFromContext(ctx); ok {
		return tx
	}

	return q.DB
}

func Connect(ctx context.Context, log logger.ILog, cfg *Config) (*pgxpool.Pool, error) {
	pc, err := pgxpool.ParseConfig(cfg.URI)
	if err != nil {
		return nil, fmt.Errorf("failed to parse connection config: %w", err)
	}

	pc.MaxConns = defaultMaxConns
	if cfg.MaxConns > 0 {
		pc.MaxConns = cfg.MaxConns
	}
	pc.MinConns = cfg.MinConns
	pc.MaxConnIdleTime = orDefault(cfg.MaxConnIdleTime, defaultMaxConnIdleTime)
	pc.MaxConnLifetime = orDefault(cfg.MaxConnLifetime, defaultMaxConnLifetime)
	pc.HealthCheckPeriod = orDefault(cfg.HealthCheckPeriod, defaultHealthCheckPeriod)

	if cfg.ApplicationName != "" {
		if pc.ConnConfig.RuntimeParams == nil {
			pc.ConnConfig.RuntimeParams = make(map[string]string)
		}
		pc.ConnConfig.RuntimeParams["application_name"] = cfg.ApplicationName
	}

	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()

		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	log.Infof("PostgreSQL connected (app=%s max=%d)", cfg.ApplicationName, pc.MaxConns)

	return pool, nil
}

func orDefault(value, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}

	return fallback
}
