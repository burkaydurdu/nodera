package app

import (
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/cmd/notification/config"
	"github.com/piggystats/nodera/contexts/notification/aggregates/access"
	"github.com/piggystats/nodera/contexts/notification/aggregates/notification"
	"github.com/piggystats/nodera/contexts/notification/api"
	v1 "github.com/piggystats/nodera/contexts/notification/api/v1"
	apiMiddleware "github.com/piggystats/nodera/contexts/notification/api/v1/middleware"
	"github.com/piggystats/nodera/contexts/notification/application/handlers"
	"github.com/piggystats/nodera/contexts/notification/async/consumers"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/internal/persistence/redis"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/echo_middleware"
	"github.com/piggystats/nodera/pkg/http"
)

// Dependencies are what the binary builds and the integration suite fakes:
// the email sender and the realtime publisher stand in for SMTP and RabbitMQ.
type Dependencies struct {
	Log      logger.ILog
	Conf     *config.Config
	Pool     *pgxpool.Pool
	Redis    redis.IService
	Email    handlers.IEmailSender
	Realtime handlers.IRealtimePublisher
	// Clock is nil outside tests.
	Clock func() time.Time
}

func (d Dependencies) now() func() time.Time {
	if d.Clock != nil {
		return d.Clock
	}

	return time.Now
}

// Services are the aggregate services over one pool.
type Services struct {
	Notifications notification.IService
	Access        access.IService
	Transactions  postgresql.ITransactionManager
}

func NewServices(pool *pgxpool.Pool) *Services {
	queries := postgresql.New(pool)

	return &Services{
		Notifications: notification.NewService(notification.NewRepository(queries)),
		Access:        access.NewService(access.NewRepository(queries)),
		Transactions:  postgresql.NewTransactionManager(pool),
	}
}

// BuildServer wires the Inbox API. The binary and the integration suite both
// go through it, so the suite exercises the same chain the binary serves.
func BuildServer(d Dependencies, rateLimit int) *echo.Echo {
	services := NewServices(d.Pool)
	inbox := handlers.NewInbox(d.Log, services.Notifications, services.Access, d.Realtime, d.now())
	authenticate := apiMiddleware.Authenticate(d.Conf.Secret.JWT, apiMiddleware.NewRedisSessions(d.Redis))

	server := http.CreateEcho(d.Log, rateLimit)
	api.InitSwagger(server)

	apiV1 := api.NewV1Routes(server)
	apiV1.Use(echo_middleware.GetCorrelationID(d.Log))
	v1.NewInbox(inbox, authenticate).SetRoutes(apiV1)

	return server
}

// BuildConsumers answers every source the worker consumes; the integration
// suite hands messages to the same ones.
func BuildConsumers(d Dependencies) *Consumers {
	services := NewServices(d.Pool)
	dispatcher := handlers.NewDispatcher(d.Log, services.Notifications, services.Access, services.Transactions, d.Email, d.Realtime, d.now())

	return &Consumers{
		Customer: consumers.NewCustomerEvents(services.Access, handlers.NewTeams(services.Access, dispatcher)),
		Emails:   consumers.NewCustomerEmails(dispatcher),
		Billing:  consumers.NewBillingEvents(dispatcher),
		Droplets: consumers.NewDropletEvents(handlers.NewDroplets(dispatcher)),
		Mail:     consumers.NewMailEvents(dispatcher, d.Conf.Domain.FrontURL()),
	}
}

type Consumers struct {
	Customer *consumers.CustomerEvents
	Emails   *consumers.CustomerEmails
	Billing  *consumers.BillingEvents
	Droplets *consumers.DropletEvents
	Mail     *consumers.MailEvents
}

func (c *Consumers) Sources() []consumers.Source {
	return []consumers.Source{c.Customer, c.Emails, c.Billing, c.Droplets, c.Mail}
}
