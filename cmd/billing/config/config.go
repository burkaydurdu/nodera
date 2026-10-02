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
	envPrefix = "NODERA_BILLING_"
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
	PDF         PDF        `mapstructure:"pdf"`
	Storage     Storage    `mapstructure:"storage"`
	Exchange    Exchange   `mapstructure:"exchange"`
	Jobs        Jobs       `mapstructure:"jobs"`
	Domain      Domain     `mapstructure:"domain"`
	Billing     Billing    `mapstructure:"billing"`
	Metrics     Metrics    `mapstructure:"metrics"`
}

// Billing.StartAt is the cutover: usage before it is never billed, so
// Droplets that ran before billing went live cost nothing for that time.
type Billing struct {
	StartAt string `mapstructure:"start_at" validate:"required,datetime=2006-01-02T15:04:05Z07:00"`
}

// Start is the parsed cutover; validation has already refused a bad value.
func (b Billing) Start() time.Time {
	at, _ := time.Parse(time.RFC3339, b.StartAt)

	return at.UTC()
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

// MongoDB holds the outbox only (docs/adr/0001).
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
// tokens with; the billing service verifies, never issues. Secret.Links signs
// the links billing emails carry (Invoice PDF downloads, payment
// confirmations), so rotating it voids every link already sent.
type Secret struct {
	JWT   string `mapstructure:"jwt" validate:"required"`
	Links string `mapstructure:"links" validate:"required"`
}

// Providers carries each Payment provider's credentials; they follow the
// usual secret path and never live in a table. Which provider a country uses
// is the payment_routes table's decision, not the config's.
type Providers struct {
	Craftgate Craftgate `mapstructure:"craftgate"`
	Stripe    Stripe    `mapstructure:"stripe"`
}

type Craftgate struct {
	Enabled            bool   `mapstructure:"enabled"`
	APIKey             string `mapstructure:"api_key"`
	SecretKey          string `mapstructure:"secret_key"`
	BaseURL            string `mapstructure:"base_url"`
	WebhookKey         string `mapstructure:"webhook_key"`
	ThreeDSCallbackKey string `mapstructure:"threeds_callback_key"`
}

type Stripe struct {
	Enabled       bool   `mapstructure:"enabled"`
	SecretKey     string `mapstructure:"secret_key"`
	WebhookSecret string `mapstructure:"webhook_secret"`
}

// PDF.GotenbergURL is the renderer Invoice PDFs go through.
type PDF struct {
	GotenbergURL string `mapstructure:"gotenberg_url"`
}

// Storage.LocalDir is where the local object store keeps rendered PDFs in
// dev and tests.
type Storage struct {
	LocalDir string `mapstructure:"local_dir"`
}

// Exchange.TCMBURL is the central bank bulletin; empty means today.xml.
type Exchange struct {
	TCMBURL string `mapstructure:"tcmb_url"`
}

type Jobs struct {
	ThresholdIntervalMinutes int `mapstructure:"threshold_interval_minutes" validate:"required,gte=1"`
	DunningIntervalMinutes   int `mapstructure:"dunning_interval_minutes" validate:"required,gte=1"`
	ReconcileIntervalMinutes int `mapstructure:"reconcile_interval_minutes" validate:"required,gte=1"`
}

func (j Jobs) ThresholdInterval() time.Duration {
	return time.Duration(j.ThresholdIntervalMinutes) * time.Minute
}

func (j Jobs) DunningInterval() time.Duration {
	return time.Duration(j.DunningIntervalMinutes) * time.Minute
}

func (j Jobs) ReconcileInterval() time.Duration {
	return time.Duration(j.ReconcileIntervalMinutes) * time.Minute
}

// Domain.Front is the web app's origin: card-verification returns and every
// link an email carries (pay, confirm, Invoice PDF) point at it.
type Domain struct {
	Front string `mapstructure:"front" validate:"required,url"`
	// Gateway is the gateway's public origin. The payment provider posts the
	// 3-D Secure result there, so it is a backend route, not a web page.
	Gateway string `mapstructure:"gateway" validate:"required,url"`
}

// FrontURL is the origin without a trailing slash, ready for a path.
func (d Domain) FrontURL() string {
	return strings.TrimRight(d.Front, "/")
}

// GatewayURL is the gateway origin without a trailing slash.
func (d Domain) GatewayURL() string {
	return strings.TrimRight(d.Gateway, "/")
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
	v.AddConfigPath("cmd/billing/.conf")
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
	// A Payment provider credential moves real money, so it stays out of the
	// log in every environment, dev included.
	printable.Providers = maskProviders(printable.Providers)
	_, err := pp.Println(printable)
	cobra.CheckErr(err)

	return &cfg
}

func maskSecrets(c Config) Config {
	for _, secret := range []*string{
		&c.PostgreSQL.URI, &c.Redis.Password, &c.MongoDB.Password, &c.MongoDB.URI,
		&c.RabbitMQ.Password, &c.RabbitMQ.AuthSecret, &c.Secret.JWT, &c.Secret.Links,
	} {
		if str.IsNotEmpty(*secret) {
			*secret = mask
		}
	}

	return c
}

func maskProviders(p Providers) Providers {
	for _, secret := range []*string{
		&p.Craftgate.APIKey, &p.Craftgate.SecretKey, &p.Craftgate.WebhookKey, &p.Craftgate.ThreeDSCallbackKey,
		&p.Stripe.SecretKey, &p.Stripe.WebhookSecret,
	} {
		if str.IsNotEmpty(*secret) {
			*secret = mask
		}
	}

	return p
}
