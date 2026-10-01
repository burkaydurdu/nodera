package postgresql

import "time"

type Config struct {
	URI               string
	ApplicationName   string
	MaxConns          int32
	MinConns          int32
	MaxConnIdleTime   time.Duration
	MaxConnLifetime   time.Duration
	HealthCheckPeriod time.Duration
}
