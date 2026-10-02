package taxrule_test

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"

	"github.com/piggystats/nodera/contexts/billing/aggregates/taxrule"
)

func TestAmount_RoundsHalfUpToCents(t *testing.T) {
	kdv := &taxrule.TaxRule{RateBps: 2000}
	assert.Equal(t, "20.00", kdv.Amount(decimal.RequireFromString("100")).StringFixed(2))
	assert.Equal(t, "0.01", kdv.Amount(decimal.RequireFromString("0.025")).StringFixed(2), "0.005 rounds up")
	assert.Equal(t, "2.47", kdv.Amount(decimal.RequireFromString("12.33")).StringFixed(2))

	exempt := &taxrule.TaxRule{RateBps: 0}
	assert.True(t, exempt.Amount(decimal.RequireFromString("512.40")).IsZero())
}

func TestLabel(t *testing.T) {
	assert.Equal(t, "KDV %20", taxrule.Label("tax.kdv_20", "tr"))
	assert.Equal(t, "Export-service exemption", taxrule.Label("tax.export_service_exemption", "en"))
	assert.Equal(t, "Export-service exemption", taxrule.Label("tax.export_service_exemption", "de"), "an unknown locale reads English")
	assert.Equal(t, "tax.unknown", taxrule.Label("tax.unknown", "tr"))
}
