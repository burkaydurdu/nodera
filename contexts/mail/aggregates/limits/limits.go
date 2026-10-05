// Package limits is what protects Nodera's shared IPs from one Project: the
// Daily limit that grows with clean sending, the daily counters, the
// seven-day rates and the Pause they can lead to.
package limits

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

const (
	InitialDailyLimit = 200
	// MaxDailyLimit keeps doubling from overflowing; the Package caps sending
	// long before it.
	MaxDailyLimit = 1_000_000
	WindowDays    = 7
	// MinWindowEmails is the volume below which rates are noise.
	MinWindowEmails = 500
	// WarnEvery spaces rate warnings out.
	WarnEvery = 24 * time.Hour
)

// The thresholds, as fractions of what was sent, kept as integer ratios so
// they are compared exactly: 2% and 5% of bounces, 0.05% and 0.1% of
// complaints.
const (
	warnBouncePer100      = 2
	pauseBouncePer100     = 5
	warnComplaintPer10000 = 5
	pauseComplaintPer1000 = 1
)

type Reason string

const (
	ReasonBounceRate    Reason = "bounce_rate"
	ReasonComplaintRate Reason = "complaint_rate"
)

// Limits is a Project's Daily limit and Pause.
type Limits struct {
	ProjectID     uuid.UUID
	TeamID        uuid.UUID
	DailyLimit    int
	PausedAt      *time.Time
	PauseReason   *Reason
	RatesWarnedAt *time.Time
	RatesResetAt  *time.Time
}

func (l *Limits) Paused() bool {
	return l.PausedAt != nil
}

// JudgedFrom is the first day the rates are judged from: the window's start,
// or the day after an operator lifted a Pause, whose earlier days would only
// Pause the Project again.
func (l *Limits) JudgedFrom(windowStart time.Time) time.Time {
	if l.RatesResetAt == nil {
		return windowStart
	}
	if after := DayOf(*l.RatesResetAt).AddDate(0, 0, 1); after.After(windowStart) {
		return after
	}

	return windowStart
}

// Day is one UTC day of a Project: the limit that held, what was sent and
// the outcomes recorded.
type Day struct {
	ProjectID   uuid.UUID
	Day         time.Time
	Limit       int
	Sent        int
	Delivered   int
	Bounced     int
	Complained  int
	EvaluatedAt *time.Time
}

// Clean is whether a finished day earns the Project a doubled Daily limit:
// it used at least half of that day's limit with rates under the warning
// line. Requiring real use stops a trickle of single emails from inflating
// the limit.
func (d Day) Clean() bool {
	return d.Sent > 0 &&
		d.Sent*2 >= d.Limit &&
		d.Bounced*100 < warnBouncePer100*d.Sent &&
		d.Complained*10000 < warnComplaintPer10000*d.Sent
}

// Window is a Project's totals over the last WindowDays days.
type Window struct {
	Sent       int
	Delivered  int
	Bounced    int
	Complained int
}

func (w Window) BounceRate() string {
	return percent(w.Bounced, w.Sent)
}

func (w Window) ComplaintRate() string {
	return percent(w.Complained, w.Sent)
}

func percent(part, whole int) string {
	if whole == 0 {
		return decimal.Zero.StringFixed(2)
	}

	return decimal.NewFromInt(int64(part)).Mul(decimal.NewFromInt(100)).Div(decimal.NewFromInt(int64(whole))).StringFixed(2)
}

// Verdict is what the rates call for.
type Verdict struct {
	Warn   bool
	Pause  bool
	Reason Reason
}

// Judge applies the thresholds to a window; bounces name the reason when
// both rates cross the line.
func Judge(w Window) Verdict {
	if w.Sent < MinWindowEmails {
		return Verdict{}
	}

	switch {
	case w.Bounced*100 >= pauseBouncePer100*w.Sent:
		return Verdict{Pause: true, Reason: ReasonBounceRate}
	case w.Complained*1000 >= pauseComplaintPer1000*w.Sent:
		return Verdict{Pause: true, Reason: ReasonComplaintRate}
	case w.Bounced*100 >= warnBouncePer100*w.Sent || w.Complained*10000 >= warnComplaintPer10000*w.Sent:
		return Verdict{Warn: true}
	default:
		return Verdict{}
	}
}

// DayOf is the UTC day holding t.
func DayOf(t time.Time) time.Time {
	u := t.UTC()

	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

// Outcome is the counter a Message event adds to.
type Outcome string

const (
	OutcomeDelivered  Outcome = "delivered"
	OutcomeBounced    Outcome = "bounced"
	OutcomeComplained Outcome = "complained"
)
