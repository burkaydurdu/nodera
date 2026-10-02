package paymentmethod_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/piggystats/nodera/contexts/billing/aggregates/paymentmethod"
)

func TestExpiresWithin(t *testing.T) {
	const month = 30 * 24 * time.Hour
	now := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)

	cases := []struct {
		name           string
		expMonth, year int
		want           bool
	}{
		{"expires end of this month", 10, 2026, true},
		{"expires end of next month is too far", 11, 2026, false},
		{"already expired", 9, 2026, false},
		{"no expiry reported", 0, 0, false},
		{"december rolls into January", 12, 2026, false},
	}
	for _, c := range cases {
		m := &paymentmethod.PaymentMethod{ExpMonth: c.expMonth, ExpYear: c.year}
		assert.Equal(t, c.want, m.ExpiresWithin(now, month), c.name)
	}

	december := &paymentmethod.PaymentMethod{ExpMonth: 12, ExpYear: 2026}
	at, ok := december.ExpiresAt()
	assert.True(t, ok)
	assert.Equal(t, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), at)
	assert.True(t, december.ExpiresWithin(time.Date(2026, 12, 2, 0, 0, 0, 0, time.UTC), month))
}
