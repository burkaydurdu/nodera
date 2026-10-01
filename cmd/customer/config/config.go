package config

import (
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/k0kubun/pp"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	customerConfig "github.com/piggystats/nodera/contexts/customer/config"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/pkg/metrics"
	"github.com/piggystats/nodera/utils/environment"
)

const (
	mask      = "********"
	envPrefix = "NODERA_CUSTOMER_"
)

type Config struct {
	Environment string     `mapstructure:"environment" validate:"required"`
	Server      Server     `mapstructure:"server"`
	PostgreSQL  PostgreSQL `mapstructure:"postgresql"`
	Redis       Redis      `mapstructure:"redis"`
	MongoDB     MongoDB    `mapstructure:"mongodb"`
	RabbitMQ    RabbitMQ   `mapstructure:"rabbitmq"`
	Secret      Secret     `mapstructure:"secret"`
	Auth        Auth       `mapstructure:"auth"`
	Domain      Domain     `mapstructure:"domain"`
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

type Secret struct {
	JWT string `mapstructure:"jwt" validate:"required"`
}

// Auth holds the session and code lifetimes. dev_code, when set, replaces
// every generated code; it is refused outside dev.
type Auth struct {
	AccessTTLMinutes int    `mapstructure:"access_ttl_minutes" validate:"required,gte=1"`
	RefreshTTLDays   int    `mapstructure:"refresh_ttl_days" validate:"required,gte=1"`
	DevCode          string `mapstructure:"dev_code"`
}

type Domain struct {
	Front string `mapstructure:"front" validate:"required,url"`
}

type Metrics struct {
	Enabled bool   `mapstructure:"enabled"`
	Port    string `mapstructure:"port"`
}

func (m Metrics) Addr() string {
	if m.Port == "" {
		return metrics.DefaultAddr
	}

	return m.Port
}

func (c *Config) IsDev() bool {
	return c.Environment == environment.Dev
}

// ToAuthConfig is the slice the customer context consumes.
func (c *Config) ToAuthConfig() customerConfig.Auth {
	devCode := ""
	if c.IsDev() {
		devCode = c.Auth.DevCode
	}

	return customerConfig.Auth{
		JWTSecret:  c.Secret.JWT,
		AccessTTL:  time.Duration(c.Auth.AccessTTLMinutes) * time.Minute,
		RefreshTTL: time.Duration(c.Auth.RefreshTTLDays) * 24 * time.Hour,
		DevCode:    devCode,
		FrontURL:   strings.TrimRight(c.Domain.Front, "/"),
	}
}

func InitConfig(configName string) *Config {
	v := viper.New()
	v.AddConfigPath("cmd/customer/.conf")
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
	_, err := pp.Println(printable)
	cobra.CheckErr(err)

	return &cfg
}

func maskSecrets(c Config) Config {
	if c.PostgreSQL.URI != "" {
		c.PostgreSQL.URI = mask
	}
	if c.Redis.Password != "" {
		c.Redis.Password = mask
	}
	if c.MongoDB.Password != "" {
		c.MongoDB.Password = mask
	}
	if c.MongoDB.URI != "" {
		c.MongoDB.URI = mask
	}
	if c.RabbitMQ.Password != "" {
		c.RabbitMQ.Password = mask
	}
	if c.RabbitMQ.AuthSecret != "" {
		c.RabbitMQ.AuthSecret = mask
	}
	if c.Secret.JWT != "" {
		c.Secret.JWT = mask
	}

	return c
}
