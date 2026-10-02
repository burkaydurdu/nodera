package usage_test

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"

	"github.com/piggystats/nodera/contexts/billing/aggregates/usage"
)

func at(day, hour, minute int) time.Time {
	return time.Date(2026, 10, day, hour, minute, 0, 0, time.UTC)
}

func ptr(t time.Time) *time.Time { return &t }

func TestHours(t *testing.T) {
	october := usage.PeriodOf(at(15, 0, 0))
	afterOctober := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name  string
		start time.Time
		stop  *time.Time
		now   time.Time
		want  int
	}{
		{"exact hours", at(2, 10, 0), ptr(at(2, 13, 0)), afterOctober, 3},
		{"a started hour counts in full", at(2, 10, 0), ptr(at(2, 10, 1)), afterOctober, 1},
		{"one minute past an hour rounds up", at(2, 10, 0), ptr(at(2, 12, 1)), afterOctober, 3},
		{"still running counts up to now", at(2, 10, 0), nil, at(2, 12, 30), 3},
		{"started now is nothing yet", at(2, 10, 0), nil, at(2, 10, 0), 0},
		{"stopped at its start is nothing", at(2, 10, 0), ptr(at(2, 10, 0)), afterOctober, 0},
		{"stop before start is nothing", at(2, 10, 0), ptr(at(2, 9, 0)), afterOctober, 0},
		{"started last month counts from the Period start", time.Date(2026, 9, 30, 22, 0, 0, 0, time.UTC), ptr(at(1, 2, 0)), afterOctober, 2},
		{"straddling the start rounds in this Period too", time.Date(2026, 9, 30, 23, 30, 0, 0, time.UTC), ptr(at(1, 0, 30)), afterOctober, 1},
		{"running past the end stops at the Period end", time.Date(2026, 10, 31, 22, 30, 0, 0, time.UTC), nil, afterOctober, 2},
		{"stopped before the Period is nothing", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), ptr(time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)), afterOctober, 0},
		{"started after the Period is nothing", time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), nil, afterOctober, 0},
		{"stopped exactly at the Period start is nothing", time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC), ptr(at(1, 0, 0)), afterOctober, 0},
		{"a full month is capped at 672", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), nil, afterOctober, usage.HourCap},
		{"672 hours exactly", at(1, 0, 0), ptr(at(29, 0, 0)), afterOctober, 672},
		{"673 started hours are capped", at(1, 0, 0), ptr(at(29, 0, 1)), afterOctober, 672},
		{"671 hours and a minute", at(1, 0, 0), ptr(at(28, 23, 1)), afterOctober, 672},
		{"670 hours and a minute", at(1, 0, 0), ptr(at(28, 22, 1)), afterOctober, 671},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, usage.Hours(c.start, c.stop, october.Start, october.End, c.now))
		})
	}
}

func TestHours_TombstoneCountsNothing(t *testing.T) {
	stopped := at(2, 10, 0)
	u := &usage.Usage{StoppedAt: &stopped}

	assert.Equal(t, 0, u.Hours(usage.PeriodOf(stopped), at(20, 0, 0)))
}

func TestLineAmount(t *testing.T) {
	cases := []struct {
		hours int
		price string
		want  string
	}{
		{3, "0.2766", "0.83"},
		{1, "0.0010", "0.01"},
		{672, "0.0744", "50.00"},
		{10, "0.5000", "5.00"},
		{0, "0.2766", "0.00"},
	}

	for _, c := range cases {
		assert.Equal(t, c.want, usage.LineAmount(c.hours, decimal.RequireFromString(c.price)).StringFixed(2), "%d × %s", c.hours, c.price)
	}
}

func TestPeriodOf(t *testing.T) {
	local := time.FixedZone("UTC+3", 3*60*60)
	p := usage.PeriodOf(time.Date(2026, 11, 1, 1, 0, 0, 0, local))

	assert.Equal(t, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), p.Start, "01:00 on the 1st in UTC+3 is still October in UTC")
	assert.Equal(t, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), p.End)
	assert.Equal(t, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), p.Previous().Start)
	assert.Equal(t, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), usage.PeriodOf(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)).Start)
	assert.Equal(t, time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC), usage.PeriodOf(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)).Previous().Start)
}
