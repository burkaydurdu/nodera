package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

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
	"github.com/piggystats/nodera/contexts/billing/async/events"
	"github.com/piggystats/nodera/contexts/billing/signing"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/locale"
	"github.com/piggystats/nodera/utils/str"
)

const (
	// settlementCurrencyTR is what a Turkish Team's Invoice is settled in
	// (ADR-0006); everyone else pays the USD total.
	settlementCurrencyTR = "TRY"

	// pdfLinkLifetime is how long the PDF link in the issue email works; the
	// web app downloads through the session at any time.
	pdfLinkLifetime = 30 * 24 * time.Hour
	// confirmLinkLifetime bounds the 3-D Secure confirmation link.
	confirmLinkLifetime = 7 * 24 * time.Hour
	// cardExpiryNotice is how far ahead a default card's expiry is warned of.
	cardExpiryNotice = 30 * 24 * time.Hour

	// batchSize bounds one reconciliation run; the rest waits for the next.
	batchSize = 100

	payPath     = "/dashboard/team/billing?team=%s&tab=invoices&invoice=%s"
	confirmPath = "/billing/confirm?payment=%s&token=%s"
	// The emailed PDF link opens a web-app page that downloads through the
	// gateway: a browser following a link cannot send the target-service header.
	pdfPath = "/billing/invoice-pdf?invoice=%s&expires=%d&token=%s"
)

// Notifier publishes the emails collection sends; every call runs after the
// change it announces has committed (docs/adr/0001).
type Notifier interface {
	InvoiceIssued(ctx context.Context, body events.InvoiceIssuedBody) error
	PaymentSucceeded(ctx context.Context, body events.PaymentSucceededBody) error
	PaymentFailed(ctx context.Context, body events.PaymentFailedBody) error
	PaymentConfirmationRequired(ctx context.Context, body events.PaymentConfirmationRequiredBody) error
	TeamSuspended(ctx context.Context, body events.TeamSuspendedBody) error
	DropletsDeletionWarning(ctx context.Context, body events.DropletsDeletionWarningBody) error
	CardExpiring(ctx context.Context, body events.CardExpiringBody) error
}

// DeletionRequester asks the droplet context to delete every Droplet of a
// Team that did not pay by day 21.
type DeletionRequester interface {
	Publish(ctx context.Context, teamID uuid.UUID, requestedAt time.Time) error
}

// Renderer turns the statement's HTML into a PDF.
type Renderer interface {
	Render(ctx context.Context, html string) ([]byte, error)
}

// ObjectStore keeps the rendered PDFs.
type ObjectStore interface {
	Put(ctx context.Context, key string, data []byte) error
	Get(ctx context.Context, key string) ([]byte, error)
}

// EInvoiceIssuer is the e-invoice integrator seam, called after issue.
type EInvoiceIssuer interface {
	Issue(ctx context.Context, inv *invoice.Invoice) (string, error)
}

// CollectionDeps is everything collection orchestrates.
type CollectionDeps struct {
	Log            logger.ILog
	Access         access.IService
	Profiles       profile.IService
	PaymentMethods paymentmethod.IService
	Statuses       billingstatus.IService
	TaxRules       taxrule.IService
	ExchangeRates  exchangerate.IService
	Usage          usage.IService
	Invoices       invoice.IService
	Payments       payment.IService
	Thresholds     threshold.IService
	Transactions   postgresql.ITransactionManager
	Registry       ProviderRegistry
	StatusChanged  StatusPublisher
	Notifier       Notifier
	Deletions      DeletionRequester
	Renderer       Renderer
	Store          ObjectStore
	EInvoice       EInvoiceIssuer
	Signer         *signing.Signer
	// FrontURL is the web app's origin; every link an email carries opens there.
	FrontURL string
	// GatewayURL is where the provider posts the 3-D Secure result: a
	// backend route reachable without the target-service header.
	GatewayURL string
	// BillingStart is the cutover: usage before it is never billed.
	BillingStart time.Time
}

// Collection turns usage into Invoices and Invoices into money: it issues,
// charges, applies the provider's answers, runs Dunning and keeps the Team's
// Billing status in step. The jobs and the HTTP handlers both go through it,
// so a Payment's outcome is applied by one path whoever asked.
type Collection struct {
	CollectionDeps
}

func NewCollection(deps CollectionDeps) *Collection {
	return &Collection{CollectionDeps: deps}
}

// Draft is the next Invoice of a Team as of windowEnd: each Droplet's hours
// in the Period that no earlier Invoice of the Period has billed, then each
// Project's Mail Package and each Storage's Package less what earlier
// Invoices of the Period charged for it. Every kind of line counts alike
// toward the Threshold, tax, Credit and the TRY equivalent.
type Draft struct {
	Period   usage.Period
	Lines    []invoice.Line
	Subtotal decimal.Decimal
}

func (c *Collection) draft(ctx context.Context, teamID uuid.UUID, period usage.Period, windowEnd time.Time) (*Draft, error) {
	rows, err := c.Usage.FindForTeam(ctx, teamID, period.Start, minTime(period.End, windowEnd))
	if err != nil {
		return nil, err
	}
	billed, err := c.Invoices.BilledHours(ctx, teamID, period.Start)
	if err != nil {
		return nil, err
	}

	lines := make([]invoice.Line, 0, len(rows))
	for _, u := range rows {
		hours := c.billableHours(u, period, windowEnd) - billed[u.DropletID]
		if hours <= 0 {
			continue
		}
		lines = append(lines, invoice.Line{
			Kind: invoice.LineDroplet, DropletID: u.DropletID, Hostname: u.Hostname, PlanCode: u.PlanCode, Hours: hours,
			HourlyPrice: u.HourlyPrice, Amount: usage.LineAmount(hours, u.HourlyPrice),
		})
	}

	mail, err := c.mailLines(ctx, teamID, period, windowEnd)
	if err != nil {
		return nil, err
	}
	lines = append(lines, mail...)

	storage, err := c.storageLines(ctx, teamID, period, windowEnd)
	if err != nil {
		return nil, err
	}
	lines = append(lines, storage...)

	return &Draft{Period: period, Lines: lines, Subtotal: invoice.Subtotal(lines)}, nil
}

// mailLines charges the Packages held in the Period from the cutover on: a
// Package held only before billing went live is never charged, like the
// hours Droplets ran then.
func (c *Collection) mailLines(ctx context.Context, teamID uuid.UUID, period usage.Period, windowEnd time.Time) ([]invoice.Line, error) {
	from, to := maxTime(period.Start, c.BillingStart), minTime(period.End, windowEnd)
	intervals, err := c.Usage.FindMailForTeam(ctx, teamID, from, to)
	if err != nil || len(intervals) == 0 {
		return nil, err
	}
	billed, err := c.Invoices.BilledMail(ctx, teamID, period.Start)
	if err != nil {
		return nil, err
	}

	mail := usage.MailLines(intervals, from, to, billed)
	lines := make([]invoice.Line, 0, len(mail))
	for _, l := range mail {
		lines = append(lines, invoice.Line{
			Kind: invoice.LineMail, ProjectID: l.ProjectID, ProjectName: l.ProjectName,
			PackageCode: l.PackageCode, Amount: l.Amount,
		})
	}

	return lines, nil
}

// storageLines charges the Packages the Team's Storages held in the Period,
// from the cutover on like Mail's.
func (c *Collection) storageLines(ctx context.Context, teamID uuid.UUID, period usage.Period, windowEnd time.Time) ([]invoice.Line, error) {
	from, to := maxTime(period.Start, c.BillingStart), minTime(period.End, windowEnd)
	intervals, err := c.Usage.FindStorageForTeam(ctx, teamID, from, to)
	if err != nil || len(intervals) == 0 {
		return nil, err
	}
	billed, err := c.Invoices.BilledStorage(ctx, teamID, period.Start)
	if err != nil {
		return nil, err
	}

	storage := usage.StorageLines(intervals, from, to, billed)
	lines := make([]invoice.Line, 0, len(storage))
	for _, l := range storage {
		lines = append(lines, invoice.Line{
			Kind: invoice.LineStorage, StorageID: l.StorageID, BucketName: l.BucketName, ProjectID: l.ProjectID,
			ProjectName: l.ProjectName, PackageCode: l.PackageCode, Amount: l.Amount,
		})
	}

	return lines, nil
}

// billableHours counts from the cutover at the earliest: what ran before
// billing went live is never billed.
func (c *Collection) billableHours(u *usage.Usage, period usage.Period, windowEnd time.Time) int {
	if u.StartedAt == nil {
		return 0
	}
	start := *u.StartedAt
	if start.Before(c.BillingStart) {
		start = c.BillingStart
	}

	return usage.Hours(start, u.StoppedAt, period.Start, period.End, windowEnd)
}

// thresholdFor ages the Team from the earlier of its creation and its first
// Droplet; a Team this context has not heard of is as new as now.
func (c *Collection) thresholdFor(ctx context.Context, teamID uuid.UUID, at time.Time) (*threshold.Threshold, error) {
	origin := at
	team, err := c.Access.FindTeam(ctx, teamID)
	if err != nil {
		return nil, err
	}
	if team != nil {
		origin = minTime(origin, team.CreatedAt)
	}
	first, err := c.Usage.FirstStartedAt(ctx, teamID)
	if err != nil {
		return nil, err
	}
	if first != nil {
		origin = minTime(origin, *first)
	}

	return c.Thresholds.For(ctx, origin, at)
}

// profileOf answers the Team's Billing profile, nil when it has none.
func (c *Collection) profileOf(ctx context.Context, teamID uuid.UUID) (*profile.Profile, error) {
	p, err := c.Profiles.FindByTeamID(ctx, teamID)
	if errors.Is(err, profile.ErrorNotFound) {
		return nil, nil
	}

	return p, err
}

// taxRuleFor looks the rule up by the profile; without one the wildcard row
// applies.
func (c *Collection) taxRuleFor(ctx context.Context, p *profile.Profile, at time.Time) (*taxrule.TaxRule, error) {
	country, accountType := taxrule.Any, taxrule.Any
	if p != nil {
		country, accountType = p.Country, string(p.AccountType)
	}

	return c.TaxRules.Lookup(ctx, country, accountType, at)
}

// localeFor is the language an Invoice of the country is written in: Turkish
// for Turkish Teams, English for everyone else.
func localeFor(country string) string {
	if country == profile.CountryTR {
		return "tr"
	}

	return locale.Default
}

// invoiceAddress is the billing profile's invoice email, told about the
// Team's money matters besides the members the notification context picks
// (docs/adr/0008); nil when the profile names none.
func (c *Collection) invoiceAddress(ctx context.Context, teamID uuid.UUID) (*events.Address, error) {
	p, err := c.profileOf(ctx, teamID)
	if err != nil {
		return nil, err
	}
	if p == nil || str.IsBlank(p.InvoiceEmail) {
		return nil, nil
	}

	return &events.Address{Email: p.InvoiceEmail, FullName: p.LegalName, Locale: localeFor(p.Country)}, nil
}

func (c *Collection) payURL(teamID, invoiceID uuid.UUID) string {
	return c.FrontURL + fmt.Sprintf(payPath, teamID, invoiceID)
}

// confirmURL is empty for an attempt that carries no confirmation expiry:
// without one no token can be verified.
func (c *Collection) confirmURL(p *payment.Payment) string {
	if p.ConfirmExpiresAt == nil {
		return ""
	}
	token := c.Signer.Sign(signing.PurposePaymentConfirm, p.ID, *p.ConfirmExpiresAt)

	return c.FrontURL + fmt.Sprintf(confirmPath, p.ID, url.QueryEscape(token))
}

func (c *Collection) pdfURL(invoiceID uuid.UUID, at time.Time) string {
	expires := at.Add(pdfLinkLifetime).Truncate(time.Second)
	token := c.Signer.Sign(signing.PurposeInvoicePDF, invoiceID, expires)

	return c.FrontURL + fmt.Sprintf(pdfPath, invoiceID, expires.Unix(), url.QueryEscape(token))
}

// logFailure records a step that failed after the change it follows was
// committed: the row holds the truth, and the step is retried or replayed.
func (c *Collection) logFailure(err error, step string, fields logger.Fields) {
	c.Log.ErrorWithFields(err, fields.Add("step", step))
}

func minTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}

	return a
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}

	return a
}
