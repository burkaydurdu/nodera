package pdf_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/piggystats/nodera/contexts/billing/pdf"
)

func statement(locale string) *pdf.Statement {
	return &pdf.Statement{
		Locale: locale, Number: "NDR-2026-000001", IssuedAt: time.Date(2026, 11, 1, 0, 5, 0, 0, time.UTC),
		PeriodStart: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), PeriodEnd: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		TeamName: "Acme", Payer: pdf.Payer{LegalName: "Acme Bilişim A.Ş.", Company: true, AddressLine1: "Moda Cad. 1", City: "İstanbul", Country: "TR", TaxID: "1234567890", TaxOffice: "Kadıköy", InvoiceEmail: "books@acme.example"},
		Lines:    []pdf.Line{{Hostname: "web-1", PlanCode: "c1", Hours: 3, HourlyPrice: "0.2766", Amount: "0.83"}},
		TaxLines: []pdf.TaxLine{{Label: "KDV %20", RateBps: 2000, Base: "100.00", Amount: "20.00"}},
		Currency: "USD", Subtotal: "100.00", TotalDue: "120.00",
		Settlement: &pdf.Settlement{Currency: "TRY", Amount: "4998.14", Rate: "41.6512", RateDate: time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)},
	}
}

func TestHTML_TurkishStatementCarriesTheLegalDetailsAndTheRate(t *testing.T) {
	html, err := pdf.HTML(statement("tr"))
	require.NoError(t, err)

	for _, want := range []string{"Hesap özeti", "fatura yerine geçmez", "NDR-2026-000001", "Acme Bilişim A.Ş.", "1234567890", "Kadıköy",
		"web-1", "KDV %20", "%20", "4998.14 TRY", "41.6512", "30.10.2026"} {
		assert.Contains(t, html, want)
	}
}

func TestHTML_EnglishIsTheDefault(t *testing.T) {
	s := statement("de")
	s.Settlement = nil
	s.TaxLines = []pdf.TaxLine{{Label: "Export-service exemption", RateBps: 0, Base: "100.00", Amount: "0.00"}}
	html, err := pdf.HTML(s)
	require.NoError(t, err)

	assert.Contains(t, html, "statement of account, not a tax invoice")
	assert.Contains(t, html, "Export-service exemption (0% of 100.00)")
	assert.NotContains(t, html, "TRY")
}

func TestTaxLineRate(t *testing.T) {
	assert.Equal(t, "20", pdf.TaxLine{RateBps: 2000}.Rate())
	assert.Equal(t, "0", pdf.TaxLine{RateBps: 0}.Rate())
	assert.Equal(t, "7.5", pdf.TaxLine{RateBps: 750}.Rate())
	assert.Equal(t, "8.25", pdf.TaxLine{RateBps: 825}.Rate())
}
