// Package invoice is a Team's statement for a Period or a Threshold: its
// lines, Tax lines, the Credit applied, the totals in USD and, for Turkish
// Teams, the TRY equivalent at the issue date's Exchange rate. It is
// numbered on issue and moves issued → paid or payment_failed → void. Until
// an e-invoice issuer is connected it is a statement, not a legal invoice.
package invoice

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type Kind string

const (
	KindPeriod    Kind = "period"
	KindThreshold Kind = "threshold"
)

// Status is persisted from issue on; the draft is computed on read and never
// stored.
type Status string

const (
	StatusIssued        Status = "issued"
	StatusPaid          Status = "paid"
	StatusPaymentFailed Status = "payment_failed"
	StatusVoid          Status = "void"
)

// Currency is what every Invoice is priced in.
const Currency = "USD"

type Invoice struct {
	ID                 uuid.UUID
	TeamID             uuid.UUID
	Kind               Kind
	Status             Status
	Number             string
	PeriodStart        time.Time
	PeriodEnd          time.Time
	WindowEnd          time.Time
	Currency           string
	Subtotal           decimal.Decimal
	TaxTotal           decimal.Decimal
	CreditApplied      decimal.Decimal
	TotalDue           decimal.Decimal
	SettlementCurrency *string
	SettlementAmount   *decimal.Decimal
	ExchangeRate       *decimal.Decimal
	ExchangeRateDate   *time.Time
	Locale             string
	Profile            ProfileSnapshot
	IssuedAt           time.Time
	PaidAt             *time.Time
	PDFKey             *string
	EInvoiceRef        *string
	FailedAt           *time.Time
	DunningStep        int
	NotifiedAt         *time.Time
	Lines              []Line
	TaxLines           []TaxLine
}

// LineKind is the product an Invoice line charges for.
type LineKind string

const (
	LineDroplet LineKind = "droplet"
	LineMail    LineKind = "mail"
)

// Line is one priced row of the Invoice: a Droplet's hours at its Locked
// price, or the Mail Package a Project held in the Period. Each kind fills
// its own fields and leaves the other's empty.
type Line struct {
	Kind   LineKind
	Amount decimal.Decimal
	// Droplet lines only.
	DropletID   uuid.UUID
	Hostname    string
	PlanCode    string
	Hours       int
	HourlyPrice decimal.Decimal
	// Mail lines only.
	ProjectID   uuid.UUID
	ProjectName string
	PackageCode string
}

// TaxLine keeps the rule's label key for the e-invoice issuer and the label
// as rendered in the Invoice's locale on the issue date.
type TaxLine struct {
	LabelKey string
	Label    string
	RateBps  int
	Base     decimal.Decimal
	Amount   decimal.Decimal
}

// ProfileSnapshot is the Billing profile as it was on the issue date; a later
// edit never rewrites an issued Invoice.
type ProfileSnapshot struct {
	AccountType  string `json:"account_type"`
	LegalName    string `json:"legal_name"`
	Country      string `json:"country"`
	AddressLine1 string `json:"address_line1"`
	AddressLine2 string `json:"address_line2"`
	City         string `json:"city"`
	PostalCode   string `json:"postal_code"`
	TaxID        string `json:"tax_id"`
	TaxOffice    string `json:"tax_office"`
	VATID        string `json:"vat_id"`
	InvoiceEmail string `json:"invoice_email"`
}

// Payable reports whether a Payment may still be attempted.
func (i *Invoice) Payable() bool {
	return i.Status == StatusIssued || i.Status == StatusPaymentFailed
}

// AmountIn answers what the Invoice costs in a card's currency: the
// settlement amount for the settlement currency, the USD total for USD.
func (i *Invoice) AmountIn(currency string) (decimal.Decimal, bool) {
	if i.SettlementCurrency != nil && *i.SettlementCurrency == currency && i.SettlementAmount != nil {
		return *i.SettlementAmount, true
	}
	if currency == i.Currency {
		return i.TotalDue, true
	}

	return decimal.Zero, false
}

// Subtotal sums the line amounts.
func Subtotal(lines []Line) decimal.Decimal {
	sum := decimal.Zero
	for _, l := range lines {
		sum = sum.Add(l.Amount)
	}

	return sum
}

// Totals is what an Invoice adds up to.
type Totals struct {
	Subtotal      decimal.Decimal
	TaxTotal      decimal.Decimal
	CreditApplied decimal.Decimal
	TotalDue      decimal.Decimal
}

// ComputeTotals applies the Credit before the card is charged: as much of the
// balance as covers subtotal and tax, never more (contract: Money rules).
func ComputeTotals(subtotal, tax, creditBalance decimal.Decimal) Totals {
	gross := subtotal.Add(tax)
	credit := decimal.Max(decimal.Zero, decimal.Min(creditBalance, gross))

	return Totals{Subtotal: subtotal, TaxTotal: tax, CreditApplied: credit, TotalDue: gross.Sub(credit)}
}

// Settlement is the total due in the settlement currency at the rate, rounded
// half-up to the kuruş.
func Settlement(totalDue, rate decimal.Decimal) decimal.Decimal {
	return totalDue.Mul(rate).Round(2)
}

// FormatNumber renders the year's sequence as NDR-YYYY-NNNNNN.
func FormatNumber(year int, sequence int64) string {
	return fmt.Sprintf("NDR-%04d-%06d", year, sequence)
}
