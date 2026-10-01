package mongo

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"

	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/utils/str"
)

// Config names the MongoDB the outbox lives in. Nothing else may use it
// (docs/adr/0001).
type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	Database string
	URI      string
}

type IClient interface {
	GetClient() *mongo.Client
	GetDatabase() *mongo.Database
}

type Client struct {
	client   *mongo.Client
	database *mongo.Database
}

func NewClient(cfg *Config, log logger.ILog) IClient {
	uri := cfg.URI
	if str.IsEmpty(uri) {
		uri = fmt.Sprintf("mongodb://%s:%d", cfg.Host, cfg.Port)
		if str.IsNotEmpty(cfg.Username) {
			uri = fmt.Sprintf("mongodb://%s:%s@%s:%d", cfg.Username, cfg.Password, cfg.Host, cfg.Port)
		}
	}

	client, err := mongo.Connect(context.Background(), options.Client().ApplyURI(uri))
	cobra.CheckErr(err)
	cobra.CheckErr(client.Ping(context.Background(), readpref.Primary()))
	log.Info("MongoDB connection successful")

	return &Client{client: client, database: client.Database(cfg.Database)}
}

func (c *Client) GetClient() *mongo.Client {
	return c.client
}

func (c *Client) GetDatabase() *mongo.Database {
	return c.database
}
