package catalog

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestHourlyPrice(t *testing.T) {
	cases := []struct {
		name   string
		offer  string
		bps    int
		expect string
	}{
		{"spec example: 0.41 + 20%", "0.41", 2000, "0.4920"},
		{"rounds up, never down", "0.0001", 2000, "0.0002"},
		{"exact multiple keeps scale", "0.5", 1000, "0.5500"},
		{"zero commission is the offer", "0.318", 0, "0.3180"},
		{"override 15%", "0.318", 1500, "0.3657"},
		{"repeating fraction rounds up", "0.098", 2000, "0.1176"},
		{"third decimal carry", "0.0925", 2000, "0.1110"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := HourlyPrice(decimal.RequireFromString(tc.offer), tc.bps)
			assert.Equal(t, tc.expect, got.StringFixed(HourlyScale))
		})
	}
}

func TestMonthlyPrice(t *testing.T) {
	cases := []struct {
		name   string
		hourly string
		hours  int
		expect string
	}{
		{"spec example: 0.4920 × 672", "0.4920", 672, "330.63"},
		{"exact cents stay", "0.5000", 672, "336.00"},
		{"sub-cent rounds up", "0.0001", 672, "0.07"},
		{"zero hours", "0.4920", 0, "0.00"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MonthlyPrice(decimal.RequireFromString(tc.hourly), tc.hours)
			assert.Equal(t, tc.expect, got.StringFixed(MonthlyScale))
		})
	}
}

func TestEffectiveCommissionBps(t *testing.T) {
	override := 1500
	assert.Equal(t, 1500, EffectiveCommissionBps(&override, 2000))
	assert.Equal(t, 2000, EffectiveCommissionBps(nil, 2000))
}
