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
	envPrefix = "NODERA_KUBERNETES_"
)

// The engine kinds a deployment may name.
const (
	EngineKamaji = "kamaji"
	EngineFake   = "fake"
)

type Config struct {
	Environment string     `mapstructure:"environment" validate:"required"`
	Server      Server     `mapstructure:"server"`
	Engine      Engine     `mapstructure:"engine"`
	Nodes       Nodes      `mapstructure:"nodes"`
	Limits      Limits     `mapstructure:"limits"`
	PostgreSQL  PostgreSQL `mapstructure:"postgresql"`
	Redis       Redis      `mapstructure:"redis"`
	MongoDB     MongoDB    `mapstructure:"mongodb"`
	RabbitMQ    RabbitMQ   `mapstructure:"rabbitmq"`
	Secret      Secret     `mapstructure:"secret"`
	Jobs        Jobs       `mapstructure:"jobs"`
	Metrics     Metrics    `mapstructure:"metrics"`
}

// Server is the management API behind the customer gateway.
type Server struct {
	Name string `mapstructure:"name" validate:"required"`
	Port string `mapstructure:"port" validate:"required"`
}

// Engine is the control plane fleet of the one Region this deployment serves
// (docs/adr/0013). Kind is "kamaji" for a real fleet and "fake" for
// development without one. EndpointDomain is Nodera's own Kubernetes domain:
// a Cluster answers at <id>.<region>.<endpoint_domain> on a port of the
// Region's address that is its alone, taken two by two (the next is its
// tunnel's) from FirstEndpointPort..LastEndpointPort (docs/adr/0015).
type Engine struct {
	Kind              string `mapstructure:"kind" validate:"required,oneof=kamaji fake"`
	Region            string `mapstructure:"region" validate:"required"`
	EndpointDomain    string `mapstructure:"endpoint_domain" validate:"required"`
	FirstEndpointPort int    `mapstructure:"first_endpoint_port" validate:"required,gte=1024,lte=65534"`
	LastEndpointPort  int    `mapstructure:"last_endpoint_port" validate:"required,gtfield=FirstEndpointPort,lte=65535"`
	TimeoutSeconds    int    `mapstructure:"timeout_seconds" validate:"required,gte=1"`
	Kamaji            Kamaji `mapstructure:"kamaji"`
}

func (e Engine) Timeout() time.Duration { return time.Duration(e.TimeoutSeconds) * time.Second }

// Kamaji is how the service reaches the management cluster: its API server,
// a bearer token of a service account allowed to manage TenantControlPlanes
// and their Secrets in Namespace, and the CA it presents. DataStore is the
// etcd pool a Region's Control planes share. AddonsDir holds the manifests
// PrepareCluster applies into every tenant (the CNI).
type Kamaji struct {
	APIServer     string `mapstructure:"api_server"`
	Token         string `mapstructure:"token"`
	CACertificate string `mapstructure:"ca_certificate"`
	Namespace     string `mapstructure:"namespace"`
	DataStore     string `mapstructure:"datastore"`
	Replicas      int    `mapstructure:"replicas"`
	AddonsDir     string `mapstructure:"addons_dir"`
}

// Nodes is what every Node runs: the Image code of the droplet catalog.
type Nodes struct {
	Image string `mapstructure:"image" validate:"required"`
}

// Limits bound what one Project may hold (docs/adr/0013).
type Limits struct {
	ClustersPerProject int `mapstructure:"clusters_per_project" validate:"required,gte=1"`
	PoolsPerCluster    int `mapstructure:"pools_per_cluster" validate:"required,gte=1"`
	NodesPerPool       int `mapstructure:"nodes_per_pool" validate:"required,gte=1"`
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

// Secret.JWT is the customer service's access-token secret; the kubernetes
// service verifies, never issues.
type Secret struct {
	JWT string `mapstructure:"jwt" validate:"required"`
}

type Jobs struct {
	ProvisionSeconds int `mapstructure:"provision_seconds" validate:"required,gte=1"`
	WatchSeconds     int `mapstructure:"watch_seconds" validate:"required,gte=1"`
	ReleaseSeconds   int `mapstructure:"release_seconds" validate:"required,gte=1"`
	DeleteSeconds    int `mapstructure:"delete_seconds" validate:"required,gte=1"`
}

func (j Jobs) Provision() time.Duration { return time.Duration(j.ProvisionSeconds) * time.Second }
func (j Jobs) Watch() time.Duration     { return time.Duration(j.WatchSeconds) * time.Second }
func (j Jobs) Release() time.Duration   { return time.Duration(j.ReleaseSeconds) * time.Second }
func (j Jobs) Delete() time.Duration    { return time.Duration(j.DeleteSeconds) * time.Second }

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
	v.AddConfigPath("cmd/kubernetes/.conf")
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
		&c.RabbitMQ.AuthSecret, &c.Secret.JWT, &c.Engine.Kamaji.Token,
	} {
		if str.IsNotEmpty(*s) {
			*s = mask
		}
	}

	return c
}
