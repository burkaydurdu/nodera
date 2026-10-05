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
	"github.com/piggystats/nodera/utils/str"
)

const (
	mask      = "********"
	envPrefix = "NODERA_INTEGRATION_GATEWAY_"
)

type Config struct {
	Environment string  `mapstructure:"environment" validate:"required"`
	Server      Server  `mapstructure:"server"`
	Redis       Redis   `mapstructure:"redis"`
	Metrics     Metrics `mapstructure:"metrics"`
}

type Server struct {
	Name string `mapstructure:"name" validate:"required"`
	Port string `mapstructure:"port" validate:"required"`
}

// Redis is the customer service's: the API keys it writes there are what
// the gateway authenticates against.
type Redis struct {
	Host     string `mapstructure:"host" validate:"required"`
	Port     int    `mapstructure:"port" validate:"required"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
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
	v.AddConfigPath("cmd/integration-gateway/.conf")
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
	if !cfg.IsDev() && str.IsNotEmpty(printable.Redis.Password) {
		printable.Redis.Password = mask
	}
	_, err := pp.Println(printable)
	cobra.CheckErr(err)

	return &cfg
}

// GatewayConfig is the YAML route table as read from disk.
type GatewayConfig struct {
	Gateway gatewayConfig.Gateway `mapstructure:"gateway"`
}

func (g *GatewayConfig) ConvertToGateway() *gatewayConfig.IntegrationGatewayConfig {
	converted, err := convert.ConvertObject[GatewayConfig, gatewayConfig.IntegrationGatewayConfig](g)
	cobra.CheckErr(err)

	return converted
}

func InitGatewayConfig(configName string) *GatewayConfig {
	v := viper.New()
	v.AddConfigPath(".conf/gateway")
	v.AddConfigPath("cmd/integration-gateway/.conf/gateway")
	v.SetConfigType("yaml")
	v.SetConfigName(configName)

	cobra.CheckErr(v.ReadInConfig())

	var gateway GatewayConfig
	cobra.CheckErr(v.Unmarshal(&gateway))

	return &gateway
}
