package app

import (
	"context"
	"net"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/cmd/mail/config"
	"github.com/piggystats/nodera/contexts/mail/aggregates/access"
	"github.com/piggystats/nodera/contexts/mail/aggregates/billing"
	"github.com/piggystats/nodera/contexts/mail/aggregates/limits"
	"github.com/piggystats/nodera/contexts/mail/aggregates/message"
	"github.com/piggystats/nodera/contexts/mail/aggregates/packages"
	"github.com/piggystats/nodera/contexts/mail/aggregates/sendingdomain"
	"github.com/piggystats/nodera/contexts/mail/aggregates/suppression"
	"github.com/piggystats/nodera/contexts/mail/aggregates/webhook"
	"github.com/piggystats/nodera/contexts/mail/api"
	"github.com/piggystats/nodera/contexts/mail/api/mtahook"
	"github.com/piggystats/nodera/contexts/mail/api/send"
	v1 "github.com/piggystats/nodera/contexts/mail/api/v1"
	apiMiddleware "github.com/piggystats/nodera/contexts/mail/api/v1/middleware"
	"github.com/piggystats/nodera/contexts/mail/apikey"
	"github.com/piggystats/nodera/contexts/mail/application/handlers"
	"github.com/piggystats/nodera/contexts/mail/application/jobs"
	"github.com/piggystats/nodera/contexts/mail/async/consumers"
	"github.com/piggystats/nodera/contexts/mail/async/producers"
	"github.com/piggystats/nodera/contexts/mail/dns"
	"github.com/piggystats/nodera/contexts/mail/mta/kumomta"
	"github.com/piggystats/nodera/contexts/mail/secrets"
	"github.com/piggystats/nodera/contexts/mail/smtp"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/internal/persistence/redis"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/echo_middleware"
	"github.com/piggystats/nodera/pkg/http"
	"github.com/piggystats/nodera/utils/str"
)

const (
	// The sending API takes a Message with base64 attachments; the gateway in
	// front of it holds each key to its own rate.
	sendingBodyLimit = "15MB"
	sendingRateLimit = 1000
	internalLimit    = "16MB"
	internalRate     = 1000
)

// Deps are what every mail process is built over; the binary and the
// integration suite pass the real ones and the fakes respectively.
type Deps struct {
	Log         logger.ILog
	Conf        *config.Config
	Pool        *pgxpool.Pool
	Redis       redis.IService
	Outbox      producers.OutboxClient
	MTA         handlers.MTA
	Resolver    handlers.Resolver
	Sealer      *secrets.Sealer
	ReturnPaths message.ReturnPaths
	Clock       func() time.Time
}

// Microseconds is the clock the context acts at: PostgreSQL keeps
// microseconds, and billing keys a Package interval by the exact instant it
// started, so every time leaves already rounded the way it is stored.
func Microseconds(now func() time.Time) handlers.Clock {
	return func() time.Time { return now().UTC().Truncate(time.Microsecond) }
}

type Services struct {
	Access         access.IService
	Billing        billing.IService
	SendingDomains sendingdomain.IService
	Packages       packages.IService
	Limits         limits.IService
	Messages       message.IService
	Suppressions   suppression.IService
	Webhooks       webhook.IService
	Transactions   postgresql.ITransactionManager
}

func NewServices(pool *pgxpool.Pool) *Services {
	queries := postgresql.New(pool)

	return &Services{
		Access:         access.NewService(access.NewRepository(queries)),
		Billing:        billing.NewService(billing.NewRepository(queries)),
		SendingDomains: sendingdomain.NewService(sendingdomain.NewRepository(queries)),
		Packages:       packages.NewService(packages.NewRepository(queries)),
		Limits:         limits.NewService(limits.NewRepository(queries)),
		Messages:       message.NewService(message.NewRepository(queries)),
		Suppressions:   suppression.NewService(suppression.NewRepository(queries)),
		Webhooks:       webhook.NewService(webhook.NewRepository(queries)),
		Transactions:   postgresql.NewTransactionManager(pool),
	}
}

// Handlers are built once per process and shared by its listeners, jobs and
// consumers.
type Handlers struct {
	Services       *Services
	Verifier       *apikey.Verifier
	Packages       *handlers.Packages
	SendingDomains *handlers.SendingDomains
	Sender         *handlers.Sender
	Dispatcher     *handlers.Dispatcher
	Events         *handlers.Events
	Logs           *handlers.Logs
	Suppressions   *handlers.Suppressions
	Webhooks       *handlers.Webhooks
	TeamBilling    *handlers.TeamBilling
	Rollover       *handlers.Rollover
	Protection     *handlers.Protection
	Deliveries     *handlers.Deliveries
	Retention      *handlers.Retention
	Operations     *handlers.Operations
}

func NewHandlers(d Deps) *Handlers {
	s := NewServices(d.Pool)
	now := Microseconds(d.Clock)
	announcer := producers.NewAnnouncer(d.Outbox)
	allowLocal := d.Conf.Webhooks.AllowLocal
	dispatcher := handlers.NewDispatcher(d.Log, s.Messages, s.Webhooks, s.Transactions, d.MTA, now)

	return &Handlers{
		Services:       s,
		Verifier:       apikey.NewVerifier(d.Redis).WithClock(now),
		Packages:       handlers.NewPackages(d.Log, s.Access, s.Billing, s.Packages, s.Limits, s.Transactions, announcer, now),
		SendingDomains: handlers.NewSendingDomains(d.Log, s.Access, s.SendingDomains, s.Transactions, d.Sealer, d.Resolver, announcer, d.Conf.DNS.Zone, now),
		Sender:         handlers.NewSender(d.Log, s.Access, s.Billing, s.SendingDomains, s.Packages, s.Limits, s.Messages, s.Suppressions, s.Webhooks, s.Transactions, dispatcher, announcer, d.ReturnPaths, now),
		Dispatcher:     dispatcher,
		Events:         handlers.NewEvents(kumomta.NewRecords(d.ReturnPaths), s.Messages, s.Suppressions, s.Limits, s.Webhooks, s.Transactions),
		Logs:           handlers.NewLogs(s.Access, s.Messages, now),
		Suppressions:   handlers.NewSuppressions(s.Access, s.Suppressions),
		Webhooks:       handlers.NewWebhooks(s.Access, s.Webhooks, d.Sealer, allowLocal, now),
		TeamBilling:    handlers.NewTeamBilling(d.Log, s.Access, s.Billing, s.Packages, s.Transactions, announcer),
		Rollover:       handlers.NewRollover(d.Log, s.Access, s.Packages, s.Transactions, announcer, now),
		Protection:     handlers.NewProtection(d.Log, s.Access, s.Limits, s.Transactions, announcer, now),
		Deliveries:     handlers.NewDeliveries(d.Log, s.Webhooks, d.Sealer, s.Transactions, allowLocal, now),
		Retention:      handlers.NewRetention(s.Messages, s.Webhooks, now),
		Operations:     handlers.NewOperations(d.Log, s.Access, s.Limits, announcer, now),
	}
}

// BuildManagement is the web app's API behind the customer gateway.
func BuildManagement(d Deps, h *Handlers, rateLimit int) *echo.Echo {
	authenticate := apiMiddleware.Authenticate(d.Conf.Secret.JWT, apiMiddleware.NewRedisSessions(d.Redis))

	server := http.CreateEcho(d.Log, rateLimit)
	api.InitSwagger(server)

	group := api.NewV1Routes(server)
	group.Use(echo_middleware.GetCorrelationID(d.Log))
	v1.NewPackages(h.Packages, authenticate).SetRoutes(group)
	v1.NewSendingDomains(h.SendingDomains, authenticate).SetRoutes(group)
	v1.NewMessages(h.Logs, authenticate).SetRoutes(group)
	v1.NewSuppressions(h.Suppressions, authenticate).SetRoutes(group)
	v1.NewWebhooks(h.Webhooks, authenticate).SetRoutes(group)

	return server
}

// BuildSending is the API software sends with, behind the integration gateway.
func BuildSending(d Deps, h *Handlers) *echo.Echo {
	server := http.CreateEchoWithBodyLimit(d.Log, sendingRateLimit, sendingBodyLimit)
	group := api.NewV1Routes(server)
	group.Use(echo_middleware.GetCorrelationID(d.Log))
	send.NewMessages(h.Sender, h.Verifier).SetRoutes(group)

	return server
}

// BuildInternal is the listener only KumoMTA reaches.
func BuildInternal(d Deps, h *Handlers) *echo.Echo {
	server := http.CreateEchoWithBodyLimit(d.Log, internalRate, internalLimit)
	mtahook.NewMTA(h.SendingDomains, h.Events, d.Conf.Internal.Token).SetRoutes(server)

	return server
}

func BuildJobs(d Deps, h *Handlers) *jobs.Jobs {
	return jobs.New(d.Log, h.SendingDomains, h.Services.SendingDomains, h.Rollover, h.Dispatcher, h.Protection, h.Deliveries, h.Retention, Microseconds(d.Clock))
}

func BuildCustomerConsumer(h *Handlers) *consumers.CustomerEvents {
	return consumers.NewCustomerEvents(h.Services.Access)
}

func BuildBillingConsumer(h *Handlers) *consumers.BillingEvents {
	return consumers.NewBillingEvents(h.TeamBilling)
}

func BuildSMTPBackend(d Deps, h *Handlers) *smtp.Backend {
	return smtp.NewBackend(d.Log, h.Sender, h.Verifier)
}

func BuildDNS(d Deps, h *Handlers) (*dns.Server, error) {
	return dns.New(dns.Config{
		Zone: d.Conf.DNS.Zone, Nameservers: d.Conf.DNS.Nameservers, Hostmaster: d.Conf.DNS.Hostmaster,
		MXHosts: d.Conf.DNS.MXHosts, SPF: d.Conf.DNS.SPF, Addresses: d.Conf.DNS.Addresses,
	}, h.Services.SendingDomains, d.Log)
}

// NewResolver is the DNS a Sending domain is checked against: the system's,
// or the one configured.
func NewResolver(address string) *net.Resolver {
	if str.IsEmpty(address) {
		return net.DefaultResolver
	}

	dialer := &net.Dialer{Timeout: 5 * time.Second}

	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, address)
		},
	}
}
