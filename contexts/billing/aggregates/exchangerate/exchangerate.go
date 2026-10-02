// Package exchangerate is the Turkish central bank's USD buying rate per day,
// stored daily and used for the TRY equivalent of an Invoice issued that day.
package exchangerate

import (
	"time"

	"github.com/shopspring/decimal"
)

const SourceTCMB = "tcmb"

// Rate is one day's rate. RateDate is the day it was published for, which
// may be earlier than the day a caller asked about (weekends, holidays).
type Rate struct {
	RateDate  time.Time
	USDBuying decimal.Decimal
	Source    string
	FetchedAt time.Time
}

// IsFallbackFor reports whether this rate stands in for a later day.
func (r *Rate) IsFallbackFor(date time.Time) bool {
	return r.RateDate.Before(Day(date))
}

// Day truncates to the UTC calendar day the table is keyed by.
func Day(t time.Time) time.Time {
	t = t.UTC()

	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
