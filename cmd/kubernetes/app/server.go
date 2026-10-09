package app

import (
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/cmd/kubernetes/config"
	"github.com/piggystats/nodera/contexts/kubernetes/aggregates/access"
	"github.com/piggystats/nodera/contexts/kubernetes/aggregates/billing"
	"github.com/piggystats/nodera/contexts/kubernetes/aggregates/catalog"
	"github.com/piggystats/nodera/contexts/kubernetes/aggregates/cluster"
	"github.com/piggystats/nodera/contexts/kubernetes/api"
	v1 "github.com/piggystats/nodera/contexts/kubernetes/api/v1"
	apiMiddleware "github.com/piggystats/nodera/contexts/kubernetes/api/v1/middleware"
	"github.com/piggystats/nodera/contexts/kubernetes/application/handlers"
	"github.com/piggystats/nodera/contexts/kubernetes/application/jobs"
	"github.com/piggystats/nodera/contexts/kubernetes/async/consumers"
	"github.com/piggystats/nodera/contexts/kubernetes/async/producers"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/internal/persistence/redis"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/echo_middleware"
	"github.com/piggystats/nodera/pkg/http"
)

// Deps are what every kubernetes process is built over; the binary and the
// integration suite pass the real ones and the fakes respectively.
type Deps struct {
	Log    logger.ILog
	Conf   *config.Config
	Pool   *pgxpool.Pool
	Redis  redis.IService
	Outbox producers.OutboxClient
	Engine handlers.Engine
	Clock  func() time.Time
}

// Microseconds is the clock the context acts at: PostgreSQL keeps
// microseconds, so every time leaves already rounded the way it is stored.
func Microseconds(now func() time.Time) handlers.Clock {
	return func() time.Time { return now().UTC().Truncate(time.Microsecond) }
}

type Services struct {
	Access       access.IService
	Billing      billing.IService
	Catalog      catalog.IService
	Clusters     cluster.IService
	Transactions postgresql.ITransactionManager
}

func NewServices(pool *pgxpool.Pool) *Services {
	queries := postgresql.New(pool)

	return &Services{
		Access:       access.NewService(access.NewRepository(queries)),
		Billing:      billing.NewService(billing.NewRepository(queries)),
		Catalog:      catalog.NewService(catalog.NewRepository(queries)),
		Clusters:     cluster.NewService(cluster.NewRepository(queries)),
		Transactions: postgresql.NewTransactionManager(pool),
	}
}

// Handlers are built once per process and shared by its listener, jobs and
// consumers.
type Handlers struct {
	Services    *Services
	Catalog     *handlers.Catalog
	Clusters    *handlers.Clusters
	NodePools   *handlers.NodePools
	Nodes       *handlers.Nodes
	TeamBilling *handlers.TeamBilling
	Provisioner *handlers.Provisioner
	Watcher     *handlers.Watcher
	Releaser    *handlers.Releaser
	Deleter     *handlers.Deleter
}

func Settings(conf *config.Config) handlers.Settings {
	return handlers.Settings{
		Region: conf.Engine.Region, EndpointDomain: conf.Engine.EndpointDomain, NodeImage: conf.Nodes.Image,
		FirstEndpointPort: conf.Engine.FirstEndpointPort, LastEndpointPort: conf.Engine.LastEndpointPort,
		ClustersPerProject: conf.Limits.ClustersPerProject, PoolsPerCluster: conf.Limits.PoolsPerCluster, NodesPerPool: conf.Limits.NodesPerPool,
	}
}

func NewHandlers(d Deps) *Handlers {
	s := NewServices(d.Pool)
	now := Microseconds(d.Clock)
	settings := Settings(d.Conf)
	life := handlers.NewLifecycle(d.Log, s.Access, s.Clusters, s.Transactions, producers.NewAnnouncer(d.Outbox), d.Engine, now)
	orders := handlers.NewOrderer(life, d.Redis, settings)
	clusters := handlers.NewClusters(life, s.Billing, s.Catalog, orders, settings)

	return &Handlers{
		Services:    s,
		Catalog:     handlers.NewCatalog(s.Catalog, settings),
		Clusters:    clusters,
		NodePools:   handlers.NewNodePools(clusters),
		Nodes:       handlers.NewNodes(life),
		TeamBilling: handlers.NewTeamBilling(life, s.Billing),
		Provisioner: handlers.NewProvisioner(life, orders),
		Watcher:     handlers.NewWatcher(life),
		Releaser:    handlers.NewReleaser(life),
		Deleter:     handlers.NewDeleter(life),
	}
}

// BuildManagement is the web app's API behind the customer gateway.
func BuildManagement(d Deps, h *Handlers, rateLimit int) *echo.Echo {
	authenticate := apiMiddleware.Authenticate(d.Conf.Secret.JWT, apiMiddleware.NewRedisSessions(d.Redis))

	server := http.CreateEcho(d.Log, rateLimit)
	api.InitSwagger(server)

	group := api.NewV1Routes(server)
	group.Use(echo_middleware.GetCorrelationID(d.Log))
	v1.NewCatalog(h.Catalog, authenticate).SetRoutes(group)
	v1.NewClusters(h.Clusters, authenticate).SetRoutes(group)
	v1.NewNodePools(h.NodePools, authenticate).SetRoutes(group)

	return server
}

func BuildJobs(d Deps, h *Handlers) *jobs.Jobs {
	return jobs.New(d.Log, h.Provisioner, h.Watcher, h.Releaser, h.Deleter)
}

func BuildCustomerConsumer(h *Handlers) *consumers.CustomerEvents {
	return consumers.NewCustomerEvents(h.Services.Access)
}

func BuildBillingConsumer(h *Handlers) *consumers.BillingEvents {
	return consumers.NewBillingEvents(h.TeamBilling)
}

func BuildDropletConsumer(h *Handlers) *consumers.DropletEvents {
	return consumers.NewDropletEvents(h.Nodes, h.Services.Catalog)
}
