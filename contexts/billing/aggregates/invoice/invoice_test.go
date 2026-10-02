package invoice_test

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"

	"github.com/piggystats/nodera/contexts/billing/aggregates/invoice"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func TestComputeTotals(t *testing.T) {
	cases := []struct {
		name                  string
		subtotal, tax, credit string
		wantCredit, wantTotal string
	}{
		{"no credit", "100.00", "20.00", "0", "0.00", "120.00"},
		{"credit smaller than the total", "100.00", "20.00", "30.00", "30.00", "90.00"},
		{"credit covers everything", "100.00", "20.00", "500.00", "120.00", "0.00"},
		{"credit equal to the total", "100.00", "20.00", "120.00", "120.00", "0.00"},
		{"tax-exempt", "512.40", "0.00", "12.40", "12.40", "500.00"},
		{"a negative balance is no credit", "10.00", "2.00", "-5.00", "0.00", "12.00"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := invoice.ComputeTotals(d(c.subtotal), d(c.tax), d(c.credit))
			assert.Equal(t, c.wantCredit, got.CreditApplied.StringFixed(2))
			assert.Equal(t, c.wantTotal, got.TotalDue.StringFixed(2))
			assert.Equal(t, c.subtotal, got.Subtotal.StringFixed(2))
			assert.Equal(t, c.tax, got.TaxTotal.StringFixed(2))
		})
	}
}

func TestSettlement_RoundsHalfUpToKurus(t *testing.T) {
	cases := []struct{ total, rate, want string }{
		{"120.00", "41.6512", "4998.14"},
		{"0.01", "41.6500", "0.42"},
		{"0.01", "41.6449", "0.42"},
		{"1.00", "41.645", "41.65"},
		{"60.00", "41.6512", "2499.07"},
	}

	for _, c := range cases {
		assert.Equal(t, c.want, invoice.Settlement(d(c.total), d(c.rate)).StringFixed(2), "%s × %s", c.total, c.rate)
	}
}

func TestSubtotal(t *testing.T) {
	lines := []invoice.Line{{Amount: d("0.83")}, {Amount: d("50.00")}, {Amount: d("0.01")}}

	assert.Equal(t, "50.84", invoice.Subtotal(lines).StringFixed(2))
	assert.True(t, invoice.Subtotal(nil).IsZero())
}

func TestFormatNumber(t *testing.T) {
	assert.Equal(t, "NDR-2026-000001", invoice.FormatNumber(2026, 1))
	assert.Equal(t, "NDR-2027-000042", invoice.FormatNumber(2027, 42))
	assert.Equal(t, "NDR-2026-1234567", invoice.FormatNumber(2026, 1234567), "the sequence widens rather than wraps")
}

func TestAmountIn(t *testing.T) {
	try := "TRY"
	settlement := d("4998.14")
	inv := &invoice.Invoice{Currency: invoice.Currency, TotalDue: d("120.00"), SettlementCurrency: &try, SettlementAmount: &settlement}

	amount, ok := inv.AmountIn("TRY")
	assert.True(t, ok)
	assert.Equal(t, "4998.14", amount.StringFixed(2))
	amount, ok = inv.AmountIn("USD")
	assert.True(t, ok)
	assert.Equal(t, "120.00", amount.StringFixed(2))
	_, ok = inv.AmountIn("EUR")
	assert.False(t, ok)
}

func TestDunningSchedule(t *testing.T) {
	failed := time.Date(2026, 11, 1, 0, 10, 0, 0, time.UTC)
	day := func(n int) time.Time { return failed.AddDate(0, 0, n) }

	cases := []struct {
		name string
		now  time.Time
		want int
	}{
		{"just failed", failed, invoice.StepNone},
		{"a second before day 1", day(1).Add(-time.Second), invoice.StepNone},
		{"day 1", day(1), invoice.StepRetryDay1},
		{"day 2", day(2), invoice.StepRetryDay1},
		{"day 3", day(3), invoice.StepRetryDay3},
		{"day 7", day(7), invoice.StepRetryDay7},
		{"day 13", day(13), invoice.StepRetryDay7},
		{"day 14", day(14), invoice.StepWarnDay14},
		{"day 20", day(20), invoice.StepWarnDay20},
		{"day 21", day(21), invoice.StepDeleteDay21},
		{"day 60", day(60), invoice.StepDeleteDay21},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, invoice.DueStep(failed, c.now), c.name)
	}

	assert.Equal(t, day(1), *invoice.NextRetryAt(failed, invoice.StepNone))
	assert.Equal(t, day(3), *invoice.NextRetryAt(failed, invoice.StepRetryDay1))
	assert.Equal(t, day(7), *invoice.NextRetryAt(failed, invoice.StepRetryDay3))
	assert.Nil(t, invoice.NextRetryAt(failed, invoice.StepRetryDay7))
	assert.Nil(t, invoice.NextRetryAt(failed, invoice.StepWarnDay14))
	assert.Equal(t, day(21), invoice.DeletionAt(failed))
	assert.True(t, invoice.IsRetry(invoice.StepRetryDay7))
	assert.False(t, invoice.IsRetry(invoice.StepWarnDay14))
	assert.False(t, invoice.IsRetry(invoice.StepNone))
}

func TestPayable(t *testing.T) {
	for status, want := range map[invoice.Status]bool{
		invoice.StatusIssued: true, invoice.StatusPaymentFailed: true, invoice.StatusPaid: false, invoice.StatusVoid: false,
	} {
		assert.Equal(t, want, (&invoice.Invoice{Status: status}).Payable(), status)
	}
}
