package packages_test

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"

	"github.com/piggystats/nodera/contexts/mail/aggregates/packages"
)

func catalogPackage(code string, emails int, price string) *packages.Package {
	return &packages.Package{Code: code, MonthlyEmails: emails, MonthlyPrice: decimal.RequireFromString(price), Active: true}
}

func holding(code string, emails int) *packages.HeldPackage {
	return &packages.HeldPackage{Code: code, MonthlyEmails: emails, MonthlyPrice: decimal.NewFromInt(int64(emails / 1000)), StartedAt: time.Now()}
}

func TestDecide(t *testing.T) {
	ten := catalogPackage("10k", 10000, "9.00")
	five := catalogPackage("5k", 5000, "5.00")
	cancellation := packages.Schedule(nil)

	cases := []struct {
		name      string
		current   *packages.HeldPackage
		scheduled *packages.Scheduled
		target    *packages.Package
		want      packages.ChangeKind
	}{
		{"first choice", nil, nil, ten, packages.ChangeStart},
		{"cancel without a Package", nil, nil, nil, packages.ChangeNothing},
		{"larger is at once", holding("5k", 5000), nil, ten, packages.ChangeUpgrade},
		{"smaller waits for the next Period", holding("10k", 10000), nil, five, packages.ChangeScheduled},
		{"cancellation waits for the next Period", holding("10k", 10000), nil, nil, packages.ChangeScheduled},
		{"the same again undoes a schedule", holding("10k", 10000), cancellation, ten, packages.ChangeUnscheduled},
		{"the same with nothing scheduled", holding("10k", 10000), nil, ten, packages.ChangeNothing},
		{"an upgrade over a scheduled cancellation", holding("5k", 5000), cancellation, ten, packages.ChangeUpgrade},
	}

	for _, c := range cases {
		assert.Equal(t, c.want, packages.Decide(c.current, c.scheduled, c.target), c.name)
	}
}

func TestSchedule_CopiesTheTermsOfTheSmallerPackage(t *testing.T) {
	scheduled := packages.Schedule(catalogPackage("5k", 5000, "5.00"))

	assert.False(t, scheduled.Cancels())
	at := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, &packages.HeldPackage{Code: "5k", MonthlyEmails: 5000, MonthlyPrice: decimal.RequireFromString("5.00"), StartedAt: at}, scheduled.HoldFrom(at))
}

func TestSchedule_OfNothingIsACancellation(t *testing.T) {
	assert.True(t, packages.Schedule(nil).Cancels())
}

func TestPeriodStart_IsTheCalendarMonthInUTC(t *testing.T) {
	istanbul := time.FixedZone("TRT", 3*60*60)
	at := time.Date(2026, 11, 1, 1, 30, 0, 0, istanbul) // still October in UTC

	assert.Equal(t, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), packages.PeriodStart(at))
	assert.Equal(t, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), packages.NextPeriodStart(at))
	assert.Equal(t, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), packages.NextPeriodStart(time.Date(2026, 12, 31, 23, 59, 0, 0, time.UTC)))
}

func TestCrossed_EachThresholdOncePerPeriod(t *testing.T) {
	p := &packages.Period{Allowance: 10000}

	near, exhausted := p.Crossed(7999, 8000)
	assert.True(t, near)
	assert.False(t, exhausted)

	near, exhausted = p.Crossed(8000, 8500)
	assert.False(t, near, "already past 80%")
	assert.False(t, exhausted)

	near, exhausted = p.Crossed(7000, 10000)
	assert.True(t, near)
	assert.True(t, exhausted)

	near, exhausted = p.Crossed(10000, 10000)
	assert.False(t, near)
	assert.False(t, exhausted)
}
