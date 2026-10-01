package config

import (
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/k0kubun/pp"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	gatewayConfig "github.com/piggystats/nodera/contexts/gateway/config"
	"github.com/piggystats/nodera/pkg/metrics"
	"github.com/piggystats/nodera/utils/convert"
	"github.com/piggystats/nodera/utils/environment"
)

const (
	mask      = "********"
	envPrefix = "NODERA_CUSTOMER_GATEWAY_"
)

type Config struct {
	Environment string  `mapstructure:"environment" validate:"required"`
	Server      Server  `mapstructure:"server"`
	Redis       Redis   `mapstructure:"redis"`
	Secret      Secret  `mapstructure:"secret"`
	CORS        CORS    `mapstructure:"cors"`
	Metrics     Metrics `mapstructure:"metrics"`
}

type Server struct {
	Name string `mapstructure:"name" validate:"required"`
	Port string `mapstructure:"port" validate:"required"`
}

type Redis struct {
	Host     string `mapstructure:"host" validate:"required"`
	Port     int    `mapstructure:"port" validate:"required"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

// Secret.JWT must equal the customer service's; SessionKeyFormat must match
// how the customer service stores sessions.
type Secret struct {
	JWT              string `mapstructure:"jwt" validate:"required"`
	SessionKeyFormat string `mapstructure:"session_key_format" validate:"required"`
}

type CORS struct {
	AllowOrigins []string `mapstructure:"allow_origins"`
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
	v.AddConfigPath("cmd/customer-gateway/.conf")
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
		printable.Secret.JWT = mask
		if printable.Redis.Password != "" {
			printable.Redis.Password = mask
		}
	}
	_, err := pp.Println(printable)
	cobra.CheckErr(err)

	return &cfg
}

// GatewayConfig is the YAML route table as read from disk.
type GatewayConfig struct {
	Gateway gatewayConfig.Gateway `mapstructure:"gateway"`
}

func (g *GatewayConfig) ConvertToGateway() *gatewayConfig.GatewayConfig {
	converted, err := convert.ConvertObject[GatewayConfig, gatewayConfig.GatewayConfig](g)
	cobra.CheckErr(err)

	return converted
}

func InitGatewayConfig(configName string) *GatewayConfig {
	v := viper.New()
	v.AddConfigPath(".conf/gateway")
	v.AddConfigPath("cmd/customer-gateway/.conf/gateway")
	v.SetConfigType("yaml")
	v.SetConfigName(configName)

	cobra.CheckErr(v.ReadInConfig())

	var gateway GatewayConfig
	cobra.CheckErr(v.Unmarshal(&gateway))

	return &gateway
}
