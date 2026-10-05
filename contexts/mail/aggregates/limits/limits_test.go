package limits_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/piggystats/nodera/contexts/mail/aggregates/limits"
)

func TestDayClean(t *testing.T) {
	cases := []struct {
		name string
		day  limits.Day
		want bool
	}{
		{"a clean day at the limit", limits.Day{Limit: 200, Sent: 200, Bounced: 3}, true},
		{"half the limit is enough", limits.Day{Limit: 200, Sent: 100}, true},
		{"less than half is not", limits.Day{Limit: 200, Sent: 99}, false},
		{"a quiet day is not", limits.Day{Limit: 400}, false},
		{"2% bounces are not", limits.Day{Limit: 200, Sent: 200, Bounced: 4}, false},
		{"0.05% complaints are not", limits.Day{Limit: 4000, Sent: 4000, Complained: 2}, false},
	}

	for _, c := range cases {
		assert.Equal(t, c.want, c.day.Clean(), c.name)
	}
}

func TestJudge(t *testing.T) {
	cases := []struct {
		name   string
		window limits.Window
		want   limits.Verdict
	}{
		{"too few emails to judge", limits.Window{Sent: 499, Bounced: 499}, limits.Verdict{}},
		{"clean", limits.Window{Sent: 1000, Bounced: 19, Complained: 0}, limits.Verdict{}},
		{"2% bounces warn", limits.Window{Sent: 1000, Bounced: 20}, limits.Verdict{Warn: true}},
		{"0.05% complaints warn", limits.Window{Sent: 10000, Complained: 5}, limits.Verdict{Warn: true}},
		{"5% bounces pause", limits.Window{Sent: 1000, Bounced: 50}, limits.Verdict{Pause: true, Reason: limits.ReasonBounceRate}},
		{"0.1% complaints pause", limits.Window{Sent: 1000, Complained: 1}, limits.Verdict{Pause: true, Reason: limits.ReasonComplaintRate}},
		{"bounces name the reason when both cross", limits.Window{Sent: 1000, Bounced: 60, Complained: 5}, limits.Verdict{Pause: true, Reason: limits.ReasonBounceRate}},
	}

	for _, c := range cases {
		assert.Equal(t, c.want, limits.Judge(c.window), c.name)
	}
}

func TestRates_AreTwoDecimalPercentages(t *testing.T) {
	w := limits.Window{Sent: 1234, Bounced: 5, Complained: 1}

	assert.Equal(t, "0.41", w.BounceRate())
	assert.Equal(t, "0.08", w.ComplaintRate())
	assert.Equal(t, "0.00", limits.Window{}.BounceRate())
}

func TestJudgedFrom_IsTheWindowsStartForAProjectNeverLifted(t *testing.T) {
	windowStart := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)

	assert.Equal(t, windowStart, (&limits.Limits{}).JudgedFrom(windowStart))
}

func TestJudgedFrom_StartsTheDayAfterAPauseWasLifted(t *testing.T) {
	lifted := time.Date(2026, 10, 12, 15, 0, 0, 0, time.UTC)
	l := limits.Limits{RatesResetAt: &lifted}

	assert.Equal(t, time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC), l.JudgedFrom(time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)))
}

func TestJudgedFrom_ForgetsALiftOlderThanTheWindow(t *testing.T) {
	windowStart := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	longAgo := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	l := limits.Limits{RatesResetAt: &longAgo}

	assert.Equal(t, windowStart, l.JudgedFrom(windowStart))
}
