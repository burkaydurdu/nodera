package redis

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
	"github.com/spf13/cobra"

	"github.com/piggystats/nodera/logger"
)

type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	DB       int
}

type IClient interface {
	GetClient() *redis.Client
}

type Client struct {
	client *redis.Client
}

func NewClient(cfg *Config, log logger.ILog) IClient {
	client := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Username: cfg.Username,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	cobra.CheckErr(client.Ping(context.Background()).Err())
	log.Info("Redis connection successful")

	return &Client{client: client}
}

func NewClientFromRedis(c *redis.Client) IClient {
	return &Client{client: c}
}

func (c *Client) GetClient() *redis.Client {
	return c.client
}
