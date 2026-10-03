package config

import (
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/k0kubun/pp"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/piggystats/nodera/pkg/metrics"
	"github.com/piggystats/nodera/utils/environment"
	"github.com/piggystats/nodera/utils/str"
)

const (
	mask      = "********"
	envPrefix = "NODERA_REALTIME_"
)

type Config struct {
	Environment string    `mapstructure:"environment" validate:"required"`
	Server      Server    `mapstructure:"server"`
	Redis       Redis     `mapstructure:"redis"`
	RabbitMQ    RabbitMQ  `mapstructure:"rabbitmq"`
	Secret      Secret    `mapstructure:"secret"`
	WebSocket   WebSocket `mapstructure:"websocket"`
	Metrics     Metrics   `mapstructure:"metrics"`
}

type Server struct {
	Name string `mapstructure:"name" validate:"required"`
	Port string `mapstructure:"port" validate:"required"`
}

// Redis holds the sessions the sockets are checked against and the pub/sub
// channel the replicas share.
type Redis struct {
	Host     string `mapstructure:"host" validate:"required"`
	Port     int    `mapstructure:"port" validate:"required"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
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
// tokens with; the realtime service verifies, never issues.
type Secret struct {
	JWT string `mapstructure:"jwt" validate:"required"`
}

type WebSocket struct {
	// OriginPatterns are the web app's origins; another site's page cannot
	// open a socket with the visitor's token.
	OriginPatterns     []string `mapstructure:"origin_patterns" validate:"required,min=1"`
	HeartbeatSeconds   int      `mapstructure:"heartbeat_seconds" validate:"required,gte=1"`
	AuthTimeoutSeconds int      `mapstructure:"auth_timeout_seconds" validate:"required,gte=1"`
}

func (w WebSocket) Heartbeat() time.Duration { return time.Duration(w.HeartbeatSeconds) * time.Second }
func (w WebSocket) AuthTimeout() time.Duration {
	return time.Duration(w.AuthTimeoutSeconds) * time.Second
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
	v.AddConfigPath("cmd/realtime/.conf")
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
		for _, secret := range []*string{&printable.Redis.Password, &printable.RabbitMQ.Password, &printable.RabbitMQ.AuthSecret, &printable.Secret.JWT} {
			if str.IsNotEmpty(*secret) {
				*secret = mask
			}
		}
	}
	_, err := pp.Println(printable)
	cobra.CheckErr(err)

	return &cfg
}
