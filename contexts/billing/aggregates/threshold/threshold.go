// Package threshold is the usage amount within a Period that triggers an
// early Invoice for a Team: lower while the Team is new, so Nodera carries
// less risk before a Team has paid anything.
package threshold

import (
	"time"

	"github.com/shopspring/decimal"
)

type Kind string

const (
	KindNew         Kind = "new"
	KindEstablished Kind = "established"

	// NewTeamMonths is how long a Team counts as new.
	NewTeamMonths = 3
)

type Threshold struct {
	Kind   Kind
	Amount decimal.Decimal
}

// KindFor answers which Threshold applies to a Team that began at `origin`:
// new for its first NewTeamMonths months, established afterwards.
func KindFor(origin, now time.Time) Kind {
	if now.Before(origin.AddDate(0, NewTeamMonths, 0)) {
		return KindNew
	}

	return KindEstablished
}

// Crossed reports whether an unbilled amount reached the Threshold.
func (t *Threshold) Crossed(unbilled decimal.Decimal) bool {
	return unbilled.GreaterThanOrEqual(t.Amount)
}
