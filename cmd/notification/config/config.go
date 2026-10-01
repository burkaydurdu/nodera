package config

import (
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/k0kubun/pp"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/piggystats/nodera/pkg/metrics"
	"github.com/piggystats/nodera/utils/environment"
)

const (
	mask      = "********"
	envPrefix = "NODERA_NOTIFICATION_"
)

type Config struct {
	Environment string   `mapstructure:"environment" validate:"required"`
	RabbitMQ    RabbitMQ `mapstructure:"rabbitmq"`
	Mail        Mail     `mapstructure:"mail"`
	SMTP        SMTP     `mapstructure:"smtp"`
	SendGrid    SendGrid `mapstructure:"sendgrid"`
	Metrics     Metrics  `mapstructure:"metrics"`
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
	if m.Port == "" {
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
		if printable.RabbitMQ.Password != "" {
			printable.RabbitMQ.Password = mask
		}
		if printable.RabbitMQ.AuthSecret != "" {
			printable.RabbitMQ.AuthSecret = mask
		}
		if printable.SMTP.Password != "" {
			printable.SMTP.Password = mask
		}
		if printable.SendGrid.APIKey != "" {
			printable.SendGrid.APIKey = mask
		}
	}
	_, err := pp.Println(printable)
	cobra.CheckErr(err)

	return &cfg
}
