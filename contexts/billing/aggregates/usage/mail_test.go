package usage_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/piggystats/nodera/contexts/billing/aggregates/usage"
	"github.com/piggystats/nodera/utils/str"
)

var (
	shop     = uuid.MustParse("6f1c4c8e-1d3b-4b8e-9a52-0d6f5b1f0a01")
	blog     = uuid.MustParse("6f1c4c8e-1d3b-4b8e-9a52-0d6f5b1f0a02")
	mailTeam = uuid.MustParse("0b8f3a77-5e1d-4c9a-8f0e-3c2d1b0a9f88")
)

func month(m time.Month) time.Time { return time.Date(2026, m, 1, 0, 0, 0, 0, time.UTC) }

func day(d int) time.Time { return at(d, 0, 0) }

func price(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func held(name, code, monthly string, start time.Time, stop *time.Time) *usage.MailInterval {
	return &usage.MailInterval{
		ProjectID: shop, TeamID: mailTeam, ProjectName: name, PackageCode: code, MonthlyEmails: 10_000,
		MonthlyPrice: price(monthly), StartedAt: start, StoppedAt: stop,
	}
}

func TestMailLines_HighestPriceInThePeriod(t *testing.T) {
	cases := []struct {
		name      string
		intervals []*usage.MailInterval
		from, to  time.Time
		billed    string
		// wantCode empty: the Project owes nothing more for the Period.
		wantCode, wantName, wantAmount string
	}{
		{
			name:      "held the whole Period",
			intervals: []*usage.MailInterval{held("Shop", "10k", "9.00", month(9), nil)},
			from:      month(10), to: month(11),
			wantCode: "10k", wantName: "Shop", wantAmount: "9.00",
		},
		{
			name:      "chosen mid-Period is not prorated",
			intervals: []*usage.MailInterval{held("Shop", "10k", "9.00", day(20), nil)},
			from:      month(10), to: month(11),
			wantCode: "10k", wantName: "Shop", wantAmount: "9.00",
		},
		{
			name:      "ended on the Period's second day is not prorated",
			intervals: []*usage.MailInterval{held("Shop", "10k", "9.00", month(9), ptr(day(2)))},
			from:      month(10), to: month(11),
			wantCode: "10k", wantName: "Shop", wantAmount: "9.00",
		},
		{
			name: "an upgrade mid-Period pays only the higher price",
			intervals: []*usage.MailInterval{
				held("Shop", "5k", "5.00", day(1), ptr(day(15))),
				held("Shop", "50k", "29.00", day(15), nil),
			},
			from: month(10), to: month(11),
			wantCode: "50k", wantName: "Shop", wantAmount: "29.00",
		},
		{
			name: "a downgrade at the boundary leaves the old price in the old Period",
			intervals: []*usage.MailInterval{
				held("Shop", "50k", "29.00", month(9), ptr(month(11))),
				held("Shop", "10k", "9.00", month(11), nil),
			},
			from: month(10), to: month(11),
			wantCode: "50k", wantName: "Shop", wantAmount: "29.00",
		},
		{
			name: "a downgrade at the boundary charges the new price in the new Period",
			intervals: []*usage.MailInterval{
				held("Shop", "50k", "29.00", month(9), ptr(month(11))),
				held("Shop", "10k", "9.00", month(11), nil),
			},
			from: month(11), to: month(12),
			wantCode: "10k", wantName: "Shop", wantAmount: "9.00",
		},
		{
			name:      "stopped exactly at the Period start is not held in it",
			intervals: []*usage.MailInterval{held("Shop", "10k", "9.00", month(9), ptr(month(10)))},
			from:      month(10), to: month(11),
		},
		{
			name:      "started exactly at the Period end is not held in it",
			intervals: []*usage.MailInterval{held("Shop", "10k", "9.00", month(11), nil)},
			from:      month(10), to: month(11),
		},
		{
			name:      "started after the window is not held yet",
			intervals: []*usage.MailInterval{held("Shop", "10k", "9.00", day(20), nil)},
			from:      month(10), to: day(10),
		},
		{
			name:      "an empty window holds nothing",
			intervals: []*usage.MailInterval{held("Shop", "10k", "9.00", month(9), nil)},
			from:      month(11), to: month(11),
		},
		{
			name:      "stopped before the Period is not held in it",
			intervals: []*usage.MailInterval{held("Shop", "10k", "9.00", month(9), ptr(time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)))},
			from:      month(10), to: month(11),
		},
		{
			name: "an interval stopped at its own start was never held",
			intervals: []*usage.MailInterval{
				held("Shop", "5k", "5.00", day(1), nil),
				held("Shop", "50k", "29.00", day(10), ptr(day(10))),
			},
			from: month(10), to: month(11),
			wantCode: "5k", wantName: "Shop", wantAmount: "5.00",
		},
		{
			name: "what an earlier Invoice of the Period charged is deducted",
			intervals: []*usage.MailInterval{
				held("Shop", "10k", "9.00", day(1), ptr(day(15))),
				held("Shop", "50k", "29.00", day(15), nil),
			},
			from: month(10), to: month(11), billed: "9.00",
			wantCode: "50k", wantName: "Shop", wantAmount: "20.00",
		},
		{
			name:      "nothing more once the highest price was charged",
			intervals: []*usage.MailInterval{held("Shop", "10k", "9.00", day(1), nil)},
			from:      month(10), to: month(11), billed: "9.00",
		},
		{
			name:      "more charged earlier than the highest price owes nothing back",
			intervals: []*usage.MailInterval{held("Shop", "10k", "9.00", day(1), nil)},
			from:      month(10), to: month(11), billed: "29.00",
		},
		{
			name:      "a free Package owes nothing",
			intervals: []*usage.MailInterval{held("Shop", "trial", "0.00", day(1), nil)},
			from:      month(10), to: month(11),
		},
		{
			name: "on a tie the latest interval names the line",
			intervals: []*usage.MailInterval{
				held("Shop", "10k", "9.00", day(1), ptr(day(5))),
				held("Shop EU", "10k", "9.00", day(10), nil),
			},
			from: month(10), to: month(11),
			wantCode: "10k", wantName: "Shop EU", wantAmount: "9.00",
		},
		{
			name: "the highest price wins whatever the order",
			intervals: []*usage.MailInterval{
				held("Shop", "100k", "49.00", day(20), nil),
				held("Shop", "5k", "5.00", day(1), ptr(day(10))),
				held("Shop", "25k", "19.00", day(10), ptr(day(20))),
			},
			from: month(10), to: month(11),
			wantCode: "100k", wantName: "Shop", wantAmount: "49.00",
		},
		{
			name: "intervals outside the Period never raise the price",
			intervals: []*usage.MailInterval{
				held("Shop", "100k", "49.00", month(9), ptr(month(10))),
				held("Shop", "10k", "9.00", month(10), ptr(month(11))),
				held("Shop", "50k", "29.00", month(11), nil),
			},
			from: month(10), to: month(11),
			wantCode: "10k", wantName: "Shop", wantAmount: "9.00",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			billed := map[uuid.UUID]decimal.Decimal{}
			if str.IsNotEmpty(c.billed) {
				billed[shop] = price(c.billed)
			}

			lines := usage.MailLines(c.intervals, c.from, c.to, billed)

			if str.IsEmpty(c.wantCode) {
				assert.Empty(t, lines)

				return
			}
			require.Len(t, lines, 1)
			assert.Equal(t, shop, lines[0].ProjectID)
			assert.Equal(t, c.wantCode, lines[0].PackageCode)
			assert.Equal(t, c.wantName, lines[0].ProjectName)
			assert.Equal(t, c.wantAmount, lines[0].Amount.StringFixed(2))
		})
	}
}

func TestMailLines_OneLinePerProjectInTheOrderTheyStarted(t *testing.T) {
	blogPackage := held("Blog", "5k", "5.00", day(3), nil)
	blogPackage.ProjectID = blog
	intervals := []*usage.MailInterval{
		held("Shop", "25k", "19.00", day(7), nil),
		blogPackage,
		held("Shop", "10k", "9.00", day(4), ptr(day(7))),
	}

	lines := usage.MailLines(intervals, month(10), month(11), map[uuid.UUID]decimal.Decimal{blog: price("5.00")})

	require.Len(t, lines, 1, "the Blog's Package was charged in full already")
	assert.Equal(t, shop, lines[0].ProjectID)
	assert.Equal(t, "19.00", lines[0].Amount.StringFixed(2))

	lines = usage.MailLines(intervals, month(10), month(11), nil)

	require.Len(t, lines, 2)
	assert.Equal(t, blog, lines[0].ProjectID, "the Blog's Package started first")
	assert.Equal(t, "5.00", lines[0].Amount.StringFixed(2))
	assert.Equal(t, shop, lines[1].ProjectID)
	assert.Equal(t, "19.00", lines[1].Amount.StringFixed(2))
}

func TestMailLines_NoIntervalsNoLines(t *testing.T) {
	assert.Empty(t, usage.MailLines(nil, month(10), month(11), nil))
}

func TestMailInterval_Overlaps(t *testing.T) {
	cases := []struct {
		name  string
		start time.Time
		stop  *time.Time
		want  bool
	}{
		{"running since before the window", month(9), nil, true},
		{"started inside the window", day(10), nil, true},
		{"stopped inside the window", month(9), ptr(day(10)), true},
		{"inside the window", day(10), ptr(day(11)), true},
		{"stopped at the window's start", month(9), ptr(month(10)), false},
		{"started at the window's end", month(11), nil, false},
		{"stopped at its own start", day(10), ptr(day(10)), false},
		{"stopped before its own start", day(10), ptr(day(9)), false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, held("Shop", "10k", "9.00", c.start, c.stop).Overlaps(month(10), month(11)))
		})
	}
}
