package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/billing/aggregates/invoice"
	"github.com/piggystats/nodera/contexts/billing/aggregates/payment"
	"github.com/piggystats/nodera/utils/str"
)

// Money on invoices is USD to the cent; hourly prices keep the catalog's four
// decimals; the exchange rate keeps the central bank's four.
const (
	centScale   = 2
	hourlyScale = 4
	rateScale   = 4
)

// UsageLine is one priced row of a draft or an issued Invoice, told apart by
// its kind. A droplet line is a Droplet's hours and carries the droplet
// fields; a mail line is the Mail Package a Project held in the Period and
// carries the project fields; a storage line is the Package one Storage held
// in the Period and carries the storage fields beside the project ones. Each
// kind omits the others' fields.
type UsageLine struct {
	Kind        string     `json:"kind" enums:"droplet,mail,storage"`
	DropletID   *uuid.UUID `json:"droplet_id,omitempty"`
	Hostname    *string    `json:"hostname,omitempty"`
	PlanCode    *string    `json:"plan_code,omitempty"`
	Hours       *int       `json:"hours,omitempty" example:"3"`
	HourlyPrice *string    `json:"hourly_price,omitempty" example:"0.2766"`
	StorageID   *uuid.UUID `json:"storage_id,omitempty"`
	BucketName  *string    `json:"bucket_name,omitempty" example:"shop-assets"`
	ProjectID   *uuid.UUID `json:"project_id,omitempty"`
	ProjectName *string    `json:"project_name,omitempty" example:"Shop"`
	PackageCode *string    `json:"package_code,omitempty" example:"10k"`
	Amount      string     `json:"amount" example:"0.83"`
}

type UsageThreshold struct {
	Amount string `json:"amount" example:"50.00"`
	Kind   string `json:"kind" enums:"new,established"`
}

// Usage is the draft Invoice of the open Period: what the next Invoice will
// carry if nothing changes, with tax estimated by today's Tax rule.
type Usage struct {
	PeriodStart    time.Time      `json:"period_start"`
	PeriodEnd      time.Time      `json:"period_end"`
	Lines          []UsageLine    `json:"lines"`
	Subtotal       string         `json:"subtotal" example:"12.40"`
	EstimatedTax   string         `json:"estimated_tax" example:"2.48"`
	EstimatedTotal string         `json:"estimated_total" example:"14.88"`
	Threshold      UsageThreshold `json:"threshold"`
}

func ToUsageLines(lines []invoice.Line) []UsageLine {
	out := make([]UsageLine, 0, len(lines))
	for _, l := range lines {
		out = append(out, toLine(l))
	}

	return out
}

func toLine(l invoice.Line) UsageLine {
	out := UsageLine{Kind: string(l.Kind), Amount: l.Amount.StringFixed(centScale)}
	switch l.Kind {
	case invoice.LineMail:
		out.ProjectID, out.ProjectName, out.PackageCode = &l.ProjectID, &l.ProjectName, &l.PackageCode

		return out
	case invoice.LineStorage:
		out.StorageID, out.BucketName = &l.StorageID, &l.BucketName
		out.ProjectID, out.ProjectName, out.PackageCode = &l.ProjectID, &l.ProjectName, &l.PackageCode

		return out
	}
	hourlyPrice := l.HourlyPrice.StringFixed(hourlyScale)
	out.DropletID, out.Hostname, out.PlanCode, out.Hours, out.HourlyPrice = &l.DropletID, &l.Hostname, &l.PlanCode, &l.Hours, &hourlyPrice

	return out
}

// InvoiceSummary is a row of the Invoices list.
type InvoiceSummary struct {
	ID                 uuid.UUID  `json:"id"`
	Number             string     `json:"number" example:"NDR-2026-000001"`
	Kind               string     `json:"kind" enums:"period,threshold"`
	Status             string     `json:"status" enums:"issued,paid,payment_failed,void"`
	PeriodStart        time.Time  `json:"period_start"`
	PeriodEnd          time.Time  `json:"period_end"`
	Currency           string     `json:"currency" example:"USD"`
	TotalDue           string     `json:"total_due" example:"120.00"`
	SettlementCurrency *string    `json:"settlement_currency" example:"TRY"`
	SettlementAmount   *string    `json:"settlement_amount" example:"4998.14"`
	IssuedAt           time.Time  `json:"issued_at"`
	PaidAt             *time.Time `json:"paid_at"`
}

type InvoiceLine = UsageLine

type TaxLine struct {
	Label   string `json:"label" example:"KDV %20"`
	RateBps int    `json:"rate_bps" example:"2000"`
	Base    string `json:"base" example:"100.00"`
	Amount  string `json:"amount" example:"20.00"`
}

// Invoice is the detail view: the summary, the breakdown and every Payment
// attempted against it.
type Invoice struct {
	InvoiceSummary
	Subtotal         string        `json:"subtotal" example:"100.00"`
	TaxTotal         string        `json:"tax_total" example:"20.00"`
	CreditApplied    string        `json:"credit_applied" example:"0.00"`
	ExchangeRate     *string       `json:"exchange_rate" example:"41.6512"`
	ExchangeRateDate *string       `json:"exchange_rate_date" example:"2026-10-30"`
	PDFAvailable     bool          `json:"pdf_available"`
	Lines            []InvoiceLine `json:"lines"`
	TaxLines         []TaxLine     `json:"tax_lines"`
	Payments         []*Payment    `json:"payments"`
}

func ToInvoiceSummary(inv *invoice.Invoice) InvoiceSummary {
	out := InvoiceSummary{
		ID: inv.ID, Number: inv.Number, Kind: string(inv.Kind), Status: string(inv.Status),
		PeriodStart: inv.PeriodStart.UTC(), PeriodEnd: inv.PeriodEnd.UTC(), Currency: inv.Currency,
		TotalDue: inv.TotalDue.StringFixed(centScale), IssuedAt: inv.IssuedAt.UTC(), PaidAt: utc(inv.PaidAt),
	}
	if inv.SettlementCurrency != nil && inv.SettlementAmount != nil {
		currency, amount := *inv.SettlementCurrency, inv.SettlementAmount.StringFixed(centScale)
		out.SettlementCurrency, out.SettlementAmount = &currency, &amount
	}

	return out
}

func ToInvoiceSummaries(invoices []*invoice.Invoice) []InvoiceSummary {
	out := make([]InvoiceSummary, 0, len(invoices))
	for _, inv := range invoices {
		out = append(out, ToInvoiceSummary(inv))
	}

	return out
}

func ToInvoice(inv *invoice.Invoice, payments []*payment.Payment) *Invoice {
	out := &Invoice{
		InvoiceSummary: ToInvoiceSummary(inv),
		Subtotal:       inv.Subtotal.StringFixed(centScale),
		TaxTotal:       inv.TaxTotal.StringFixed(centScale),
		CreditApplied:  inv.CreditApplied.StringFixed(centScale),
		PDFAvailable:   inv.PDFKey != nil,
		Lines:          ToUsageLines(inv.Lines),
		TaxLines:       make([]TaxLine, 0, len(inv.TaxLines)),
		Payments:       ToPayments(payments),
	}
	if inv.ExchangeRate != nil && inv.ExchangeRateDate != nil {
		rate, date := inv.ExchangeRate.StringFixed(rateScale), inv.ExchangeRateDate.UTC().Format(time.DateOnly)
		out.ExchangeRate, out.ExchangeRateDate = &rate, &date
	}
	for _, t := range inv.TaxLines {
		out.TaxLines = append(out.TaxLines, TaxLine{Label: t.Label, RateBps: t.RateBps, Base: t.Base.StringFixed(centScale), Amount: t.Amount.StringFixed(centScale)})
	}

	return out
}

// Payment is one attempt; the confirmation URL is the bank's 3-D Secure page
// while the attempt awaits the cardholder.
type Payment struct {
	ID              uuid.UUID `json:"id"`
	InvoiceID       uuid.UUID `json:"invoice_id"`
	Status          string    `json:"status" enums:"pending,succeeded,failed,confirmation_required"`
	Amount          string    `json:"amount" example:"4998.14"`
	Currency        string    `json:"currency" example:"TRY"`
	FailureReason   *string   `json:"failure_reason" example:"insufficient_funds"`
	ConfirmationURL *string   `json:"confirmation_url"`
	CreatedAt       time.Time `json:"created_at"`
}

func ToPayment(p *payment.Payment) *Payment {
	out := &Payment{
		ID: p.ID, InvoiceID: p.InvoiceID, Status: string(p.Status), Amount: p.Amount.StringFixed(centScale),
		Currency: p.Currency, CreatedAt: p.CreatedAt.UTC(),
	}
	if str.IsNotEmpty(p.FailureReason) {
		reason := p.FailureReason
		out.FailureReason = &reason
	}
	if p.Status == payment.StatusConfirmationRequired && str.IsNotEmpty(p.ConfirmationURL) {
		url := p.ConfirmationURL
		out.ConfirmationURL = &url
	}

	return out
}

func ToPayments(payments []*payment.Payment) []*Payment {
	out := make([]*Payment, 0, len(payments))
	for _, p := range payments {
		out = append(out, ToPayment(p))
	}

	return out
}

// ConfirmationStart is the bank's 3-D Secure step: an HTML document to
// render as the page, or a URL to send the browser to; one of them is set.
type ConfirmationStart struct {
	HTMLContent *string `json:"html_content"`
	RedirectURL *string `json:"redirect_url"`
}
