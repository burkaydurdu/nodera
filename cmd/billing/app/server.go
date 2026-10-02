package app

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/cmd/billing/config"
	"github.com/piggystats/nodera/contexts/billing/aggregates/access"
	"github.com/piggystats/nodera/contexts/billing/aggregates/billingstatus"
	"github.com/piggystats/nodera/contexts/billing/aggregates/exchangerate"
	"github.com/piggystats/nodera/contexts/billing/aggregates/invoice"
	"github.com/piggystats/nodera/contexts/billing/aggregates/payment"
	"github.com/piggystats/nodera/contexts/billing/aggregates/paymentmethod"
	"github.com/piggystats/nodera/contexts/billing/aggregates/profile"
	"github.com/piggystats/nodera/contexts/billing/aggregates/taxrule"
	"github.com/piggystats/nodera/contexts/billing/aggregates/threshold"
	"github.com/piggystats/nodera/contexts/billing/aggregates/usage"
	"github.com/piggystats/nodera/contexts/billing/api"
	v1 "github.com/piggystats/nodera/contexts/billing/api/v1"
	apiMiddleware "github.com/piggystats/nodera/contexts/billing/api/v1/middleware"
	"github.com/piggystats/nodera/contexts/billing/application/handlers"
	"github.com/piggystats/nodera/contexts/billing/application/jobs"
	"github.com/piggystats/nodera/contexts/billing/async/consumers"
	"github.com/piggystats/nodera/contexts/billing/async/producers"
	"github.com/piggystats/nodera/contexts/billing/einvoice"
	"github.com/piggystats/nodera/contexts/billing/pdf"
	"github.com/piggystats/nodera/contexts/billing/providers"
	"github.com/piggystats/nodera/contexts/billing/signing"
	"github.com/piggystats/nodera/contexts/billing/storage"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/internal/persistence/redis"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/echo_middleware"
	"github.com/piggystats/nodera/pkg/http"
	"github.com/piggystats/nodera/pkg/ratelimit"
)

// Dependencies are what the binary builds from config and the integration
// suite builds from containers and fakes; every Build function takes them,
// so both exercise the same chain.
type Dependencies struct {
	Log      logger.ILog
	Conf     *config.Config
	Pool     *pgxpool.Pool
	Redis    redis.IService
	Outbox   producers.OutboxClient
	Registry *providers.Registry
	// RateFetcher reads the central bank's bulletin; the suite fakes it.
	RateFetcher exchangerate.Fetcher
	// Clock is the time every rule reads; nil means time.Now.
	Clock func() time.Time
	// Renderer and Store produce and keep Invoice PDFs; nil means Gotenberg
	// at pdf.gotenberg_url and the local directory at storage.local_dir.
	Renderer pdf.Renderer
	Store    storage.Store
}

func (d Dependencies) renderer() pdf.Renderer {
	if d.Renderer == nil {
		return pdf.NewGotenberg(d.Conf.PDF.GotenbergURL)
	}

	return d.Renderer
}

func (d Dependencies) store() storage.Store {
	if d.Store == nil {
		return storage.NewLocal(d.Conf.Storage.LocalDir)
	}

	return d.Store
}

func (d Dependencies) now() func() time.Time {
	if d.Clock == nil {
		return time.Now
	}

	return d.Clock
}

// Services are the aggregate services over one pool; the server, the jobs
// and the consumers share them.
type Services struct {
	Access         access.IService
	Profiles       profile.IService
	PaymentMethods paymentmethod.IService
	Statuses       billingstatus.IService
	TaxRules       taxrule.IService
	ExchangeRates  exchangerate.IService
	Payments       payment.IService
	Usage          usage.IService
	Invoices       invoice.IService
	Thresholds     threshold.IService
	Transactions   postgresql.ITransactionManager
}

func NewServices(pool *pgxpool.Pool) *Services {
	queries := postgresql.New(pool)

	return &Services{
		Access:         access.NewService(access.NewRepository(queries)),
		Profiles:       profile.NewService(profile.NewRepository(queries)),
		PaymentMethods: paymentmethod.NewService(paymentmethod.NewRepository(queries)),
		Statuses:       billingstatus.NewService(billingstatus.NewRepository(queries)),
		TaxRules:       taxrule.NewService(taxrule.NewRepository(queries)),
		ExchangeRates:  exchangerate.NewService(exchangerate.NewRepository(queries)),
		Payments:       payment.NewService(payment.NewRepository(queries)),
		Usage:          usage.NewService(usage.NewRepository(queries)),
		Invoices:       invoice.NewService(invoice.NewRepository(queries)),
		Thresholds:     threshold.NewService(threshold.NewRepository(queries)),
		Transactions:   postgresql.NewTransactionManager(pool),
	}
}

// Handlers are the request-side rules and Collection, the business side the
// jobs share with them.
type Handlers struct {
	Authorizer    *handlers.Authorizer
	Profile       *handlers.Profile
	PaymentMethod *handlers.PaymentMethod
	Status        *handlers.Status
	Webhook       *handlers.Webhook
	Usage         *handlers.Usage
	Invoice       *handlers.Invoice
	Payment       *handlers.Payment
	Collection    *handlers.Collection
	StatusChanged *producers.TeamBillingStatusChanged
}

func NewHandlers(d Dependencies, services *Services) *Handlers {
	now := d.now()
	authorizer := handlers.NewAuthorizer(services.Access)
	statusChanged := producers.NewTeamBillingStatusChanged(d.Outbox)
	limiter := ratelimit.NewLimiter(d.Redis)
	collection := handlers.NewCollection(handlers.CollectionDeps{
		Log: d.Log, Access: services.Access, Profiles: services.Profiles, PaymentMethods: services.PaymentMethods,
		Statuses: services.Statuses, TaxRules: services.TaxRules, ExchangeRates: services.ExchangeRates,
		Usage: services.Usage, Invoices: services.Invoices, Payments: services.Payments, Thresholds: services.Thresholds,
		Transactions: services.Transactions, Registry: d.Registry, StatusChanged: statusChanged,
		Notifier: producers.NewNotifications(d.Outbox), Deletions: producers.NewTeamDropletsDeletionRequested(d.Outbox),
		Renderer: d.renderer(), Store: d.store(), EInvoice: einvoice.None{}, Signer: signing.New(d.Conf.Secret.Links),
		FrontURL: d.Conf.Domain.FrontURL(), GatewayURL: d.Conf.Domain.GatewayURL(), BillingStart: d.Conf.Billing.Start(),
	})

	return &Handlers{
		Authorizer: authorizer,
		Profile:    handlers.NewProfile(authorizer, services.Profiles, services.PaymentMethods, services.Statuses, services.Transactions, now),
		PaymentMethod: handlers.NewPaymentMethod(
			d.Log, authorizer, services.Access, services.Profiles, services.PaymentMethods, services.Statuses,
			d.Registry, services.Transactions, statusChanged, services.Invoices, d.Conf.Domain.FrontURL(), now,
		),
		Status:        handlers.NewStatus(authorizer, services.Statuses),
		Webhook:       handlers.NewWebhook(d.Log, services.Payments, d.Registry, limiter),
		Usage:         handlers.NewUsage(authorizer, collection, now),
		Invoice:       handlers.NewInvoice(authorizer, collection, limiter, now),
		Payment:       handlers.NewPayment(collection, now),
		Collection:    collection,
		StatusChanged: statusChanged,
	}
}

// BuildServer wires the billing service over already-built dependencies.
func BuildServer(d Dependencies, rateLimit int) *echo.Echo {
	services := NewServices(d.Pool)
	h := NewHandlers(d, services)

	authenticate := apiMiddleware.Authenticate(d.Conf.Secret.JWT, apiMiddleware.NewRedisSessions(d.Redis))

	server := http.CreateEcho(d.Log, rateLimit)
	api.InitSwagger(server)

	apiV1 := api.NewV1Routes(server)
	apiV1.Use(echo_middleware.GetCorrelationID(d.Log))
	v1.NewBilling(v1.Handlers{
		Profiles: h.Profile, Methods: h.PaymentMethod, Status: h.Status, Webhooks: h.Webhook,
		Usage: h.Usage, Invoices: h.Invoice, Payments: h.Payment,
	}, authenticate).SetRoutes(apiV1)

	return server
}

func BuildJobs(d Dependencies) *jobs.Jobs {
	services := NewServices(d.Pool)

	return jobs.New(d.Log, services.ExchangeRates, d.RateFetcher, NewHandlers(d, services).Collection, d.now())
}

// BuildConsumers answers every event consumer the worker binds.
func BuildConsumers(d Dependencies) []consumers.Source {
	return []consumers.Source{BuildCustomerConsumer(d), BuildUsageConsumer(d)}
}

func BuildUsageConsumer(d Dependencies) *consumers.UsageEvents {
	return consumers.NewUsageEvents(NewServices(d.Pool).Usage)
}

func BuildCustomerConsumer(d Dependencies) *consumers.CustomerEvents {
	return consumers.NewCustomerEvents(NewServices(d.Pool).Access)
}

// ReplayStatuses republishes every Team's Billing status through the outbox.
func ReplayStatuses(ctx context.Context, d Dependencies) (int, error) {
	services := NewServices(d.Pool)

	return handlers.NewReplayStatus(services.Statuses, producers.NewTeamBillingStatusChanged(d.Outbox)).Run(ctx)
}
