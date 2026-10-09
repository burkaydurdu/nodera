package app

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/cmd/droplet/config"
	"github.com/piggystats/nodera/contexts/droplet/aggregates/access"
	"github.com/piggystats/nodera/contexts/droplet/aggregates/billing"
	"github.com/piggystats/nodera/contexts/droplet/aggregates/catalog"
	"github.com/piggystats/nodera/contexts/droplet/aggregates/droplet"
	"github.com/piggystats/nodera/contexts/droplet/aggregates/provider"
	"github.com/piggystats/nodera/contexts/droplet/api"
	v1 "github.com/piggystats/nodera/contexts/droplet/api/v1"
	apiMiddleware "github.com/piggystats/nodera/contexts/droplet/api/v1/middleware"
	"github.com/piggystats/nodera/contexts/droplet/application/handlers"
	"github.com/piggystats/nodera/contexts/droplet/application/jobs"
	"github.com/piggystats/nodera/contexts/droplet/async/consumers"
	"github.com/piggystats/nodera/contexts/droplet/async/producers"
	"github.com/piggystats/nodera/contexts/droplet/providers"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/internal/persistence/redis"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/echo_middleware"
	"github.com/piggystats/nodera/pkg/http"
	"github.com/piggystats/nodera/pkg/ratelimit"
)

// Services are the aggregate services over one pool; the server, the jobs
// and the consumer share them.
type Services struct {
	Providers    provider.IService
	Catalog      catalog.IService
	Droplets     droplet.IService
	Access       access.IService
	Billing      billing.IService
	Transactions postgresql.ITransactionManager
}

func NewServices(pool *pgxpool.Pool) *Services {
	queries := postgresql.New(pool)

	return &Services{
		Providers:    provider.NewService(provider.NewRepository(queries)),
		Catalog:      catalog.NewService(catalog.NewRepository(queries)),
		Droplets:     droplet.NewService(droplet.NewRepository(queries)),
		Access:       access.NewService(access.NewRepository(queries)),
		Billing:      billing.NewService(billing.NewRepository(queries)),
		Transactions: postgresql.NewTransactionManager(pool),
	}
}

// NewMeter is the one announcer of the meter's boundaries; the server, the
// jobs and the billing reactions each write through the same outbox.
func NewMeter(log logger.ILog, outboxClient producers.OutboxClient) *handlers.Meter {
	return handlers.NewMeter(log, producers.NewBillingStarted(outboxClient), producers.NewBillingStopped(outboxClient))
}

// NewStatusAnnouncer is the one announcer of status changes, shared the same
// way as the Meter.
func NewStatusAnnouncer(log logger.ILog, outboxClient producers.OutboxClient) *handlers.StatusAnnouncer {
	return handlers.NewStatusAnnouncer(log, producers.NewStatusChanged(outboxClient))
}

// NewNodeRejected is the one announcer of a Node request this context could
// not open a Droplet for (docs/adr/0014).
func NewNodeRejected(outboxClient producers.OutboxClient) *producers.NodeRejected {
	return producers.NewNodeRejected(outboxClient)
}

// BuildServer wires the droplet service over already-built dependencies. The
// binary and the integration suite both go through it, so the suite exercises
// the same chain the binary serves.
func BuildServer(
	log logger.ILog,
	conf *config.Config,
	pool *pgxpool.Pool,
	redisService redis.IService,
	registry *providers.Registry,
	outboxClient producers.OutboxClient,
	rateLimit int,
) *echo.Echo {
	services := NewServices(pool)

	catalogHandler := handlers.NewCatalog(services.Catalog)
	dropletHandler := handlers.NewDroplet(
		log, services.Catalog, services.Droplets, services.Access, services.Billing, services.Providers, registry, services.Transactions,
		NewMeter(log, outboxClient), NewStatusAnnouncer(log, outboxClient), ratelimit.NewLimiter(redisService),
	)

	authenticate := apiMiddleware.Authenticate(conf.Secret.JWT, apiMiddleware.NewRedisSessions(redisService))

	server := http.CreateEcho(log, rateLimit)
	api.InitSwagger(server)

	apiV1 := api.NewV1Routes(server)
	apiV1.Use(echo_middleware.GetCorrelationID(log))
	v1.NewCatalog(catalogHandler, authenticate).SetRoutes(apiV1)
	v1.NewDroplet(dropletHandler, authenticate).SetRoutes(apiV1)

	return server
}

func BuildJobs(log logger.ILog, pool *pgxpool.Pool, registry *providers.Registry, outboxClient producers.OutboxClient) *jobs.Jobs {
	services := NewServices(pool)

	return jobs.New(
		log, services.Providers, services.Catalog, services.Droplets, services.Billing, registry, services.Transactions,
		NewMeter(log, outboxClient), NewStatusAnnouncer(log, outboxClient), producers.NewCatalogChanged(outboxClient),
	)
}

// BuildKubernetesConsumer opens and releases Node Droplets at the kubernetes
// context's request (docs/adr/0014).
func BuildKubernetesConsumer(log logger.ILog, conf *config.Config, pool *pgxpool.Pool, redisService redis.IService, registry *providers.Registry, outboxClient producers.OutboxClient) *consumers.KubernetesEvents {
	services := NewServices(pool)
	droplets := handlers.NewDroplet(
		log, services.Catalog, services.Droplets, services.Access, services.Billing, services.Providers, registry, services.Transactions,
		NewMeter(log, outboxClient), NewStatusAnnouncer(log, outboxClient), ratelimit.NewLimiter(redisService),
	)

	return consumers.NewKubernetesEvents(handlers.NewNodes(droplets, redisService, NewNodeRejected(outboxClient), conf.Nodes.OpsPublicKey))
}

func BuildCustomerConsumer(pool *pgxpool.Pool) *consumers.CustomerEvents {
	return consumers.NewCustomerEvents(NewServices(pool).Access)
}

func BuildBillingConsumer(log logger.ILog, pool *pgxpool.Pool, registry *providers.Registry, outboxClient producers.OutboxClient) *consumers.BillingEvents {
	services := NewServices(pool)

	return consumers.NewBillingEvents(handlers.NewTeamBilling(
		log, services.Billing, services.Droplets, services.Providers, registry, services.Transactions,
		NewMeter(log, outboxClient), NewStatusAnnouncer(log, outboxClient),
	))
}
