// Package usage is what a Team used and pays for. The hours a Droplet was
// rented are learned from the droplet context's DropletBillingStarted and
// DropletBillingStopped events and priced at the Droplet's Locked price, one
// row per Droplet. The Packages a Project held are learned from the mail
// context's MailPackageStarted and MailPackageStopped events and the
// Packages a Storage held from the storage context's StoragePackageStarted
// and StoragePackageStopped, one row per interval. What a Period owes is
// derived from all three on read.
package usage

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// HourCap is the most a Droplet is billed in one Period: four weeks, so a
// full month costs what the catalog's monthly price said.
const HourCap = 672

// Usage is one Droplet's rental. StartedAt nil marks a tombstone: the stop
// arrived before the start and the row counts for nothing until it does.
type Usage struct {
	DropletID   uuid.UUID
	TeamID      uuid.UUID
	ProjectID   uuid.UUID
	Hostname    string
	PlanCode    string
	HourlyPrice decimal.Decimal
	StartedAt   *time.Time
	StoppedAt   *time.Time
}

// Hours is the Droplet's usage in the Period as of now.
func (u *Usage) Hours(period Period, now time.Time) int {
	if u.StartedAt == nil {
		return 0
	}

	return Hours(*u.StartedAt, u.StoppedAt, period.Start, period.End, now)
}

// Hours is the overlap of [start, stop or now) with [periodStart, periodEnd)
// in whole hours, a partial hour counting as a full one, capped at HourCap.
// Each Period rounds on its own, so an hour straddling midnight UTC at the
// month's end is a started hour in both.
func Hours(start time.Time, stop *time.Time, periodStart, periodEnd, now time.Time) int {
	end := periodEnd
	if now.Before(end) {
		end = now
	}
	if stop != nil && stop.Before(end) {
		end = *stop
	}
	from := start
	if periodStart.After(from) {
		from = periodStart
	}
	if !end.After(from) {
		return 0
	}

	elapsed := end.Sub(from)
	hours := int(elapsed / time.Hour)
	if elapsed%time.Hour != 0 {
		hours++
	}

	return min(hours, HourCap)
}

// LineAmount is hours × hourly price rounded up to the cent (contract: Money
// rules); a fraction of a cent is never given away.
func LineAmount(hours int, hourlyPrice decimal.Decimal) decimal.Decimal {
	return decimal.NewFromInt(int64(hours)).Mul(hourlyPrice).RoundCeil(2)
}

// Period is a calendar month in UTC: [Start, End).
type Period struct {
	Start time.Time
	End   time.Time
}

// PeriodOf answers the Period an instant falls in.
func PeriodOf(t time.Time) Period {
	t = t.UTC()
	start := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)

	return Period{Start: start, End: start.AddDate(0, 1, 0)}
}

// Previous answers the Period before this one.
func (p Period) Previous() Period {
	return PeriodOf(p.Start.Add(-time.Nanosecond))
}
