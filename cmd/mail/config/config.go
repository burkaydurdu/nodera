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
	envPrefix = "NODERA_MAIL_"
)

type Config struct {
	Environment string     `mapstructure:"environment" validate:"required"`
	Server      Server     `mapstructure:"server"`
	SMTP        SMTP       `mapstructure:"smtp"`
	DNS         DNS        `mapstructure:"dns"`
	MTA         MTA        `mapstructure:"mta"`
	Internal    Internal   `mapstructure:"internal"`
	PostgreSQL  PostgreSQL `mapstructure:"postgresql"`
	Redis       Redis      `mapstructure:"redis"`
	MongoDB     MongoDB    `mapstructure:"mongodb"`
	RabbitMQ    RabbitMQ   `mapstructure:"rabbitmq"`
	Secret      Secret     `mapstructure:"secret"`
	Webhooks    Webhooks   `mapstructure:"webhooks"`
	Jobs        Jobs       `mapstructure:"jobs"`
	Metrics     Metrics    `mapstructure:"metrics"`
}

// Server is three listeners: the management API behind the customer
// gateway, the sending API behind the integration gateway, and the internal
// API only KumoMTA reaches.
type Server struct {
	Name         string `mapstructure:"name" validate:"required"`
	Port         string `mapstructure:"port" validate:"required"`
	SendingPort  string `mapstructure:"sending_port" validate:"required"`
	InternalPort string `mapstructure:"internal_port" validate:"required"`
}

// SMTP is the relay: STARTTLS on the submission and alternative ports,
// implicit TLS on the TLS port.
type SMTP struct {
	Hostname        string `mapstructure:"hostname" validate:"required"`
	SubmissionAddr  string `mapstructure:"submission_addr" validate:"required"`
	TLSAddr         string `mapstructure:"tls_addr" validate:"required"`
	AltAddr         string `mapstructure:"alt_addr" validate:"required"`
	CertFile        string `mapstructure:"cert_file"`
	KeyFile         string `mapstructure:"key_file"`
	MaxMessageBytes int64  `mapstructure:"max_message_bytes" validate:"required,gte=1"`
}

// DNS is the zone of docs/adr/0011 and the resolver Sending domains are
// checked against (empty for the system's).
type DNS struct {
	ListenAddr     string   `mapstructure:"listen_addr" validate:"required"`
	Zone           string   `mapstructure:"zone" validate:"required"`
	Nameservers    []string `mapstructure:"nameservers"`
	Hostmaster     string   `mapstructure:"hostmaster" validate:"required"`
	MXHosts        []string `mapstructure:"mx_hosts"`
	SPF            string   `mapstructure:"spf"`
	Addresses      []string `mapstructure:"addresses"`
	RefreshSeconds int      `mapstructure:"refresh_seconds" validate:"required,gte=1"`
	Resolver       string   `mapstructure:"resolver"`
}

func (d DNS) Refresh() time.Duration { return time.Duration(d.RefreshSeconds) * time.Second }

// MTA is KumoMTA's injection API and the credentials its policy accepts.
type MTA struct {
	InjectionURL   string `mapstructure:"injection_url" validate:"required"`
	Username       string `mapstructure:"username"`
	Password       string `mapstructure:"password"`
	TimeoutSeconds int    `mapstructure:"timeout_seconds" validate:"required,gte=1"`
}

// Internal.Token is what KumoMTA presents to the internal API.
type Internal struct {
	Token string `mapstructure:"token"`
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

type RabbitMQ struct {
	Host        string `mapstructure:"host" validate:"required"`
	Username    string `mapstructure:"username"`
	Password    string `mapstructure:"password"`
	VirtualHost string `mapstructure:"virtualhost"`
	AuthSecret  string `mapstructure:"auth_secret"`
}

// Secret.JWT is the customer service's access-token secret; the mail service
// verifies, never issues. EncryptionKey (32 bytes, base64) seals the DKIM
// keys and Webhook secrets.
type Secret struct {
	JWT           string `mapstructure:"jwt" validate:"required"`
	EncryptionKey string `mapstructure:"encryption_key" validate:"required"`
}

// Webhooks.AllowLocal lets development and test point a Webhook at loopback.
type Webhooks struct {
	AllowLocal bool `mapstructure:"allow_local"`
}

type Jobs struct {
	DomainCheckSeconds int `mapstructure:"domain_check_seconds" validate:"required,gte=1"`
	RotationMinutes    int `mapstructure:"rotation_minutes" validate:"required,gte=1"`
	RolloverMinutes    int `mapstructure:"rollover_minutes" validate:"required,gte=1"`
	DispatchSeconds    int `mapstructure:"dispatch_seconds" validate:"required,gte=1"`
	DaysMinutes        int `mapstructure:"days_minutes" validate:"required,gte=1"`
	RatesMinutes       int `mapstructure:"rates_minutes" validate:"required,gte=1"`
	WebhooksSeconds    int `mapstructure:"webhooks_seconds" validate:"required,gte=1"`
	ExpireMinutes      int `mapstructure:"expire_minutes" validate:"required,gte=1"`
}

func (j Jobs) DomainCheck() time.Duration { return time.Duration(j.DomainCheckSeconds) * time.Second }
func (j Jobs) Rotation() time.Duration    { return time.Duration(j.RotationMinutes) * time.Minute }
func (j Jobs) Rollover() time.Duration    { return time.Duration(j.RolloverMinutes) * time.Minute }
func (j Jobs) Dispatch() time.Duration    { return time.Duration(j.DispatchSeconds) * time.Second }
func (j Jobs) Days() time.Duration        { return time.Duration(j.DaysMinutes) * time.Minute }
func (j Jobs) Rates() time.Duration       { return time.Duration(j.RatesMinutes) * time.Minute }
func (j Jobs) Webhooks() time.Duration    { return time.Duration(j.WebhooksSeconds) * time.Second }
func (j Jobs) Expire() time.Duration      { return time.Duration(j.ExpireMinutes) * time.Minute }

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
	v.AddConfigPath("cmd/mail/.conf")
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
	for _, s := range []*string{
		&c.PostgreSQL.URI, &c.Redis.Password, &c.MongoDB.Password, &c.MongoDB.URI, &c.RabbitMQ.Password,
		&c.RabbitMQ.AuthSecret, &c.Secret.JWT, &c.Secret.EncryptionKey, &c.MTA.Password, &c.Internal.Token,
	} {
		if str.IsNotEmpty(*s) {
			*s = mask
		}
	}

	return c
}
