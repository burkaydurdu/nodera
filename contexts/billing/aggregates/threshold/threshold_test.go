package threshold_test

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"

	"github.com/piggystats/nodera/contexts/billing/aggregates/threshold"
)

func TestKindFor(t *testing.T) {
	origin := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)

	assert.Equal(t, threshold.KindNew, threshold.KindFor(origin, origin))
	assert.Equal(t, threshold.KindNew, threshold.KindFor(origin, time.Date(2026, 10, 15, 11, 59, 59, 0, time.UTC)))
	assert.Equal(t, threshold.KindEstablished, threshold.KindFor(origin, time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)))
}

func TestCrossed(t *testing.T) {
	fifty := &threshold.Threshold{Kind: threshold.KindNew, Amount: decimal.RequireFromString("50.00")}

	assert.True(t, fifty.Crossed(decimal.RequireFromString("50.00")))
	assert.True(t, fifty.Crossed(decimal.RequireFromString("50.01")))
	assert.False(t, fifty.Crossed(decimal.RequireFromString("49.99")))
}
