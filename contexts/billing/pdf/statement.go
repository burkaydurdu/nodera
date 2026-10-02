package pdf

import (
	"bytes"
	"embed"
	"html/template"
	"time"

	"github.com/shopspring/decimal"
)

// The statement wording stays until an e-invoice issuer is connected
// (ADR-0006); each locale has its own template so the sentences read
// naturally rather than being stitched from fragments.
//
//go:embed templates/*.html
var templateFiles embed.FS

var templates = template.Must(template.New("statement").Funcs(template.FuncMap{
	"date": func(layout string, t time.Time) string { return t.UTC().Format(layout) },
}).ParseFS(templateFiles, "templates/*.html"))

// Statement is everything the template prints, already formatted where the
// format is the service's decision (money as decimal strings).
type Statement struct {
	Locale      string
	Number      string
	Threshold   bool
	IssuedAt    time.Time
	PeriodStart time.Time
	PeriodEnd   time.Time
	TeamName    string
	Payer       Payer
	Lines       []Line
	TaxLines    []TaxLine
	Currency    string
	Subtotal    string
	Credit      string
	HasCredit   bool
	TotalDue    string
	Settlement  *Settlement
}

type Payer struct {
	LegalName    string
	Company      bool
	AddressLine1 string
	AddressLine2 string
	City         string
	PostalCode   string
	Country      string
	TaxID        string
	TaxOffice    string
	VATID        string
	InvoiceEmail string
}

type Line struct {
	Hostname    string
	PlanCode    string
	Hours       int
	HourlyPrice string
	Amount      string
}

type TaxLine struct {
	Label   string
	RateBps int
	Base    string
	Amount  string
}

// Rate renders the basis points as a percentage without trailing zeros.
func (t TaxLine) Rate() string {
	return decimal.New(int64(t.RateBps), -2).String()
}

// Settlement is the TRY equivalent of a Turkish Team's Invoice and the rate
// that produced it.
type Settlement struct {
	Currency string
	Amount   string
	Rate     string
	RateDate time.Time
}

// HTML renders the statement in its locale; an unknown locale is English.
func HTML(s *Statement) (string, error) {
	name := "statement.en.html"
	if s.Locale == "tr" {
		name = "statement.tr.html"
	}

	var out bytes.Buffer
	if err := templates.ExecuteTemplate(&out, name, s); err != nil {
		return "", err
	}

	return out.String(), nil
}
