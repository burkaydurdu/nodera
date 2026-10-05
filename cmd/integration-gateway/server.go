package main

import (
	"context"

	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/cmd/integration-gateway/config"
	"github.com/piggystats/nodera/contexts/gateway/api"
	apiMiddleware "github.com/piggystats/nodera/contexts/gateway/api/middleware"
	gatewayConfig "github.com/piggystats/nodera/contexts/gateway/config"
	"github.com/piggystats/nodera/internal/persistence/redis"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/echo_middleware"
	"github.com/piggystats/nodera/pkg/metrics"
	"github.com/piggystats/nodera/pkg/recover"
)

func CreateServer(conf *config.Config, gateway *config.GatewayConfig) (*echo.Echo, error) {
	log := logger.New(conf.Environment)
	defer recover.Init(log)

	if conf.Metrics.Enabled {
		metrics.Serve(context.Background(), log, conf.Metrics.Addr())
	}

	redisService := redis.NewService(redis.NewClient(&redis.Config{
		Host:     conf.Redis.Host,
		Port:     conf.Redis.Port,
		Username: conf.Redis.Username,
		Password: conf.Redis.Password,
		DB:       conf.Redis.DB,
	}, log))

	routeTable := gateway.ConvertToGateway()
	routeTable.InitEndpoints()

	return BuildServer(log, routeTable, redisService, apiMiddleware.NewIntegrationReverseProxy(log))
}

// BuildServer wires the gateway chain over already-built dependencies, so the
// integration test can hand it a stub upstream and a test Redis. The order is
// the contract: an unlisted route is a 404 before any key is read, and a key
// is authenticated and authorized before it spends its budget or is marked
// as used.
func BuildServer(
	log logger.ILog,
	routeTable *gatewayConfig.IntegrationGatewayConfig,
	cache apiMiddleware.IntegrationCache,
	newProxy apiMiddleware.NewReverseProxy,
) (*echo.Echo, error) {
	proxies, err := apiMiddleware.SetupIntegrationProxies(routeTable, newProxy)
	if err != nil {
		return nil, err
	}

	server := api.CreateIntegrationEcho(log)
	server.Use(echo_middleware.SetStartTime)
	server.Use(echo_middleware.CorrelationID)
	server.Use(apiMiddleware.ResolveIntegrationRoute(routeTable))
	server.Use(apiMiddleware.APIKey(cache))
	server.Use(apiMiddleware.APIKeyAuthorization())
	server.Use(apiMiddleware.APIKeyRateLimit(cache, log))
	server.Use(apiMiddleware.RecordAPIKeyUse(cache, log))
	server.Use(apiMiddleware.Proxy(proxies))

	return server, nil
}
