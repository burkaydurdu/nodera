package config

import (
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/k0kubun/pp"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/pkg/metrics"
	"github.com/piggystats/nodera/utils/environment"
	"github.com/piggystats/nodera/utils/str"
)

const (
	mask      = "********"
	envPrefix = "NODERA_DROPLET_"
)

type Config struct {
	Environment string     `mapstructure:"environment" validate:"required"`
	Server      Server     `mapstructure:"server"`
	PostgreSQL  PostgreSQL `mapstructure:"postgresql"`
	Redis       Redis      `mapstructure:"redis"`
	MongoDB     MongoDB    `mapstructure:"mongodb"`
	RabbitMQ    RabbitMQ   `mapstructure:"rabbitmq"`
	Secret      Secret     `mapstructure:"secret"`
	Providers   Providers  `mapstructure:"providers"`
	Jobs        Jobs       `mapstructure:"jobs"`
	Metrics     Metrics    `mapstructure:"metrics"`
}

type Server struct {
	Name string `mapstructure:"name" validate:"required"`
	Port string `mapstructure:"port" validate:"required"`
}

type PostgreSQL struct {
	URI  string `mapstructure:"uri" validate:"required"`
	Pool PGPool `mapstructure:"pool"`
}

type PGPool struct {
	MaxConns                 int32 `mapstructure:"max_conns"`
	MinConns                 int32 `mapstructure:"min_conns"`
	MaxConnIdleSeconds       int   `mapstructure:"max_conn_idle_seconds"`
	MaxConnLifetimeSeconds   int   `mapstructure:"max_conn_lifetime_seconds"`
	HealthCheckPeriodSeconds int   `mapstructure:"health_check_period_seconds"`
}

func (p PostgreSQL) ToDBConfig(appName string) *postgresql.Config {
	return &postgresql.Config{
		URI:               p.URI,
		ApplicationName:   appName,
		MaxConns:          p.Pool.MaxConns,
		MinConns:          p.Pool.MinConns,
		MaxConnIdleTime:   time.Duration(p.Pool.MaxConnIdleSeconds) * time.Second,
		MaxConnLifetime:   time.Duration(p.Pool.MaxConnLifetimeSeconds) * time.Second,
		HealthCheckPeriod: time.Duration(p.Pool.HealthCheckPeriodSeconds) * time.Second,
	}
}

type Redis struct {
	Host     string `mapstructure:"host" validate:"required"`
	Port     int    `mapstructure:"port" validate:"required"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

// MongoDB backs the outbox and nothing else (docs/adr/0001).
type MongoDB struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	Database string `mapstructure:"database" validate:"required"`
	URI      string `mapstructure:"uri"`
}

// RabbitMQ.Host is "host:port"; the client builds the amqp URL from it, the
// credentials and the virtual host.
type RabbitMQ struct {
	Host        string `mapstructure:"host" validate:"required"`
	Username    string `mapstructure:"username"`
	Password    string `mapstructure:"password"`
	VirtualHost string `mapstructure:"virtualhost"`
	AuthSecret  string `mapstructure:"auth_secret"`
}

// Secret.JWT is the same signing secret the customer service issues access
// tokens with; the droplet service verifies, never issues.
type Secret struct {
	JWT string `mapstructure:"jwt" validate:"required"`
}

// Providers carries each Provider's credentials; they follow the usual secret
// path and never live in a table.
type Providers struct {
	Cherry Cherry `mapstructure:"cherry"`
}

type Cherry struct {
	Enabled   bool   `mapstructure:"enabled"`
	Token     string `mapstructure:"token"`
	ProjectID int    `mapstructure:"project_id"`
	Endpoint  string `mapstructure:"endpoint"`
}

type Jobs struct {
	SyncIntervalMinutes int `mapstructure:"sync_interval_minutes" validate:"required,gte=1"`
	PollIntervalSeconds int `mapstructure:"poll_interval_seconds" validate:"required,gte=1"`
}

func (j Jobs) SyncInterval() time.Duration { return time.Duration(j.SyncIntervalMinutes) * time.Minute }
func (j Jobs) PollInterval() time.Duration { return time.Duration(j.PollIntervalSeconds) * time.Second }

type Metrics struct {
	Enabled bool   `mapstructure:"enabled"`
	Port    string `mapstructure:"port"`
}

func (m Metrics) Addr() string {
	if str.IsEmpty(m.Port) {
		return metrics.DefaultAddr
	}

	return m.Port
}

func (c *Config) IsDev() bool {
	return c.Environment == environment.Dev
}

func InitConfig(configName string) *Config {
	v := viper.New()
	v.AddConfigPath("cmd/droplet/.conf")
	v.AddConfigPath(".conf")
	v.SetConfigType("toml")
	v.SetConfigName(configName)
	v.AutomaticEnv()

	cobra.CheckErr(v.ReadInConfig())

	// viper binds an env override only for keys present in the loaded TOML,
	// so every key must exist (empty is fine) in every tracked TOML.
	for _, key := range v.AllKeys() {
		_ = v.BindEnv(key, strings.ToUpper(envPrefix+strings.ReplaceAll(key, ".", "_")))
	}

	var cfg Config
	cobra.CheckErr(v.Unmarshal(&cfg))
	cobra.CheckErr(validator.New().Struct(cfg))

	printable := cfg
	if !cfg.IsDev() {
		printable = maskSecrets(cfg)
	}
	// A Provider credential is a third party's key that spends real money, so it
	// stays out of the log in every environment, dev included.
	if str.IsNotEmpty(printable.Providers.Cherry.Token) {
		printable.Providers.Cherry.Token = mask
	}
	_, err := pp.Println(printable)
	cobra.CheckErr(err)

	return &cfg
}

func maskSecrets(c Config) Config {
	if str.IsNotEmpty(c.PostgreSQL.URI) {
		c.PostgreSQL.URI = mask
	}
	if str.IsNotEmpty(c.Redis.Password) {
		c.Redis.Password = mask
	}
	if str.IsNotEmpty(c.MongoDB.Password) {
		c.MongoDB.Password = mask
	}
	if str.IsNotEmpty(c.MongoDB.URI) {
		c.MongoDB.URI = mask
	}
	if str.IsNotEmpty(c.RabbitMQ.Password) {
		c.RabbitMQ.Password = mask
	}
	if str.IsNotEmpty(c.RabbitMQ.AuthSecret) {
		c.RabbitMQ.AuthSecret = mask
	}
	if str.IsNotEmpty(c.Secret.JWT) {
		c.Secret.JWT = mask
	}
	if str.IsNotEmpty(c.Providers.Cherry.Token) {
		c.Providers.Cherry.Token = mask
	}

	return c
}
