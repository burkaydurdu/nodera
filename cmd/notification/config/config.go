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
	envPrefix = "NODERA_NOTIFICATION_"
)

type Config struct {
	Environment string     `mapstructure:"environment" validate:"required"`
	Server      Server     `mapstructure:"server"`
	PostgreSQL  PostgreSQL `mapstructure:"postgresql"`
	Redis       Redis      `mapstructure:"redis"`
	RabbitMQ    RabbitMQ   `mapstructure:"rabbitmq"`
	Secret      Secret     `mapstructure:"secret"`
	Mail        Mail       `mapstructure:"mail"`
	SMTP        SMTP       `mapstructure:"smtp"`
	SendGrid    SendGrid   `mapstructure:"sendgrid"`
	Metrics     Metrics    `mapstructure:"metrics"`
}

// Server is the Inbox API.
type Server struct {
	Name string `mapstructure:"name" validate:"required"`
	Port string `mapstructure:"port" validate:"required"`
}

// PostgreSQL holds the Notifications and the Team read model.
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

// Redis is where the customer service keeps sessions; the Inbox API checks
// that the caller's is still alive.
type Redis struct {
	Host     string `mapstructure:"host" validate:"required"`
	Port     int    `mapstructure:"port" validate:"required"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

// Secret.JWT is the same signing secret the customer service issues access
// tokens with; the notification service verifies, never issues.
type Secret struct {
	JWT string `mapstructure:"jwt" validate:"required"`
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

type Mail struct {
	From         string `mapstructure:"from" validate:"required"`
	FromName     string `mapstructure:"from_name"`
	TemplatesDir string `mapstructure:"templates_dir" validate:"required"`
	// Provider is smtp or sendgrid.
	Provider string `mapstructure:"provider" validate:"required,oneof=smtp sendgrid"`
}

type SMTP struct {
	Host               string `mapstructure:"host"`
	Port               int    `mapstructure:"port"`
	Username           string `mapstructure:"username"`
	Password           string `mapstructure:"password"`
	InsecureSkipVerify bool   `mapstructure:"insecure_skip_verify"`
}

type SendGrid struct {
	APIKey string `mapstructure:"api_key"`
}

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
	v.AddConfigPath("cmd/notification/.conf")
	v.AddConfigPath(".conf")
	v.SetConfigType("toml")
	v.SetConfigName(configName)
	v.AutomaticEnv()

	cobra.CheckErr(v.ReadInConfig())

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
	for _, secret := range []*string{
		&c.PostgreSQL.URI, &c.Redis.Password, &c.RabbitMQ.Password, &c.RabbitMQ.AuthSecret,
		&c.Secret.JWT, &c.SMTP.Password, &c.SendGrid.APIKey,
	} {
		if str.IsNotEmpty(*secret) {
			*secret = mask
		}
	}

	return c
}
