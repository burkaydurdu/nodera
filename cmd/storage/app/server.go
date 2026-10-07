package app

import (
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/cmd/storage/config"
	"github.com/piggystats/nodera/contexts/storage/aggregates/access"
	"github.com/piggystats/nodera/contexts/storage/aggregates/accesskey"
	"github.com/piggystats/nodera/contexts/storage/aggregates/billing"
	"github.com/piggystats/nodera/contexts/storage/aggregates/catalog"
	"github.com/piggystats/nodera/contexts/storage/aggregates/storage"
	"github.com/piggystats/nodera/contexts/storage/aggregates/usage"
	"github.com/piggystats/nodera/contexts/storage/api"
	v1 "github.com/piggystats/nodera/contexts/storage/api/v1"
	apiMiddleware "github.com/piggystats/nodera/contexts/storage/api/v1/middleware"
	"github.com/piggystats/nodera/contexts/storage/application/handlers"
	"github.com/piggystats/nodera/contexts/storage/application/jobs"
	"github.com/piggystats/nodera/contexts/storage/async/consumers"
	"github.com/piggystats/nodera/contexts/storage/async/producers"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/internal/persistence/redis"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/echo_middleware"
	"github.com/piggystats/nodera/pkg/http"
	"github.com/piggystats/nodera/pkg/secrets"
)

// Deps are what every storage process is built over; the binary and the
// integration suite pass the real ones and the fakes respectively.
type Deps struct {
	Log    logger.ILog
	Conf   *config.Config
	Pool   *pgxpool.Pool
	Redis  redis.IService
	Outbox producers.OutboxClient
	Engine handlers.Engine
	Sealer *secrets.Sealer
	Clock  func() time.Time
}

// Microseconds is the clock the context acts at: PostgreSQL keeps
// microseconds, and billing keys a Package interval by the exact instant it
// started, so every time leaves already rounded the way it is stored.
func Microseconds(now func() time.Time) handlers.Clock {
	return func() time.Time { return now().UTC().Truncate(time.Microsecond) }
}

type Services struct {
	Access       access.IService
	Billing      billing.IService
	Catalog      catalog.IService
	Storages     storage.IService
	AccessKeys   accesskey.IService
	Usage        usage.IService
	Transactions postgresql.ITransactionManager
}

func NewServices(pool *pgxpool.Pool) *Services {
	queries := postgresql.New(pool)

	return &Services{
		Access:       access.NewService(access.NewRepository(queries)),
		Billing:      billing.NewService(billing.NewRepository(queries)),
		Catalog:      catalog.NewService(catalog.NewRepository(queries)),
		Storages:     storage.NewService(storage.NewRepository(queries)),
		AccessKeys:   accesskey.NewService(accesskey.NewRepository(queries)),
		Usage:        usage.NewService(usage.NewRepository(queries)),
		Transactions: postgresql.NewTransactionManager(pool),
	}
}

// Handlers are built once per process and shared by its listener, jobs and
// consumers.
type Handlers struct {
	Services    *Services
	Catalog     *handlers.Catalog
	Storages    *handlers.Storages
	AccessKeys  *handlers.AccessKeys
	Objects     *handlers.Objects
	Usage       *handlers.Usage
	TeamBilling *handlers.TeamBilling
	Operations  *handlers.Operations
	Measurer    *handlers.Measurer
	Rollover    *handlers.Rollover
	Purger      *handlers.Purger
	Reconciler  *handlers.Reconciler
}

func NewHandlers(d Deps) *Handlers {
	s := NewServices(d.Pool)
	now := Microseconds(d.Clock)
	announcer := producers.NewAnnouncer(d.Outbox)
	accounts := handlers.NewAccounts(s.Storages, d.Engine, d.Sealer)
	sync := handlers.NewSyncer(d.Log, s.Storages, accounts, d.Engine, s.Transactions)
	addresses := handlers.NewAddresses(d.Conf.Engine.PublicEndpoint, d.Conf.Engine.PathStyle)
	storages := handlers.NewStorages(d.Log, s.Access, s.Billing, s.Catalog, s.Storages, s.AccessKeys, s.Transactions, announcer, d.Engine, accounts, sync, addresses, now)
	purger := handlers.NewPurger(d.Log, s.Storages, s.AccessKeys, s.Usage, accounts, d.Engine, s.Transactions, announcer, now)

	return &Handlers{
		Services:    s,
		Catalog:     handlers.NewCatalog(s.Catalog),
		Storages:    storages,
		AccessKeys:  handlers.NewAccessKeys(d.Log, s.Access, s.Storages, s.AccessKeys, d.Engine, accounts, announcer, now),
		Objects:     handlers.NewObjects(s.Access, s.Storages, d.Engine, accounts, addresses, now),
		Usage:       handlers.NewUsage(s.Access, s.Storages, s.Usage, now),
		TeamBilling: handlers.NewTeamBilling(d.Log, s.Billing, s.Storages, storages, sync, s.Transactions, announcer),
		Operations:  handlers.NewOperations(d.Log, s.Access, s.Storages, s.Transactions, announcer, sync, now),
		Measurer:    handlers.NewMeasurer(d.Log, s.Access, s.Storages, s.Usage, accounts, d.Engine, s.Transactions, announcer, now),
		Rollover:    handlers.NewRollover(d.Log, s.Access, s.Storages, s.Transactions, announcer, sync, now),
		Purger:      purger,
		Reconciler:  handlers.NewReconciler(s.Storages, sync, purger, now),
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
	v1.NewStorages(h.Storages, authenticate).SetRoutes(group)
	v1.NewAccessKeys(h.AccessKeys, authenticate).SetRoutes(group)
	v1.NewObjects(h.Objects, authenticate).SetRoutes(group)
	v1.NewUsage(h.Usage, authenticate).SetRoutes(group)

	return server
}

func BuildJobs(d Deps, h *Handlers) *jobs.Jobs {
	return jobs.New(d.Log, h.Measurer, h.Rollover, h.Purger, h.Reconciler)
}

func BuildCustomerConsumer(h *Handlers) *consumers.CustomerEvents {
	return consumers.NewCustomerEvents(h.Services.Access)
}

func BuildBillingConsumer(h *Handlers) *consumers.BillingEvents {
	return consumers.NewBillingEvents(h.TeamBilling)
}
