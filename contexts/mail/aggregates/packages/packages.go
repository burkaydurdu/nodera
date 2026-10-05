// Package packages is the Package: the monthly allowance of email a Project
// is on and its price, the catalog it is chosen from, and the Period rows
// that count what the allowance has carried.
package packages

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// NoneCode is what a Team chooses to cancel its Package.
const NoneCode = "none"

// PriceScale is the two decimals a monthly price carries on the wire.
const PriceScale = 2

// NearLimitPercent is where a Team is told its allowance is running out.
const NearLimitPercent = 80

// Package is one row of the catalog.
type Package struct {
	Code          string
	MonthlyEmails int
	MonthlyPrice  decimal.Decimal
	Active        bool
}

// HeldPackage is the Package a Project is on, its price copied at the time
// it was chosen like a Droplet's Locked price.
type HeldPackage struct {
	Code          string
	MonthlyEmails int
	MonthlyPrice  decimal.Decimal
	StartedAt     time.Time
}

// ProjectPackage is a Project's Package and the change its next Period starts
// with. Current is nil for a Project without a Package, Scheduled for one
// without a change waiting.
type ProjectPackage struct {
	ProjectID uuid.UUID
	TeamID    uuid.UUID
	Current   *HeldPackage
	Scheduled *Scheduled
}

// Scheduled is a smaller Package or a cancellation (NoneCode) the next
// Period starts with. A Package's terms are copied when it is chosen, like
// any Package's, so a change to the catalog before the Period turns does not
// reach it.
type Scheduled struct {
	Code          string
	MonthlyEmails int
	MonthlyPrice  decimal.Decimal
}

// Schedule is what choosing target, or nil to cancel, leaves for the next
// Period.
func Schedule(target *Package) *Scheduled {
	if target == nil {
		return &Scheduled{Code: NoneCode}
	}

	return &Scheduled{Code: target.Code, MonthlyEmails: target.MonthlyEmails, MonthlyPrice: target.MonthlyPrice}
}

func (s *Scheduled) Cancels() bool {
	return s.Code == NoneCode
}

// HoldFrom starts holding the scheduled Package at the Period's first
// instant.
func (s *Scheduled) HoldFrom(at time.Time) *HeldPackage {
	return &HeldPackage{Code: s.Code, MonthlyEmails: s.MonthlyEmails, MonthlyPrice: s.MonthlyPrice, StartedAt: at}
}

// Period is one calendar month (UTC) of a Project's allowance.
type Period struct {
	ProjectID           uuid.UUID
	Start               time.Time
	Allowance           int
	Used                int
	NearLimitNotifiedAt *time.Time
	ExhaustedNotifiedAt *time.Time
}

// End is the first instant of the next Period.
func (p *Period) End() time.Time {
	return NextPeriodStart(p.Start)
}

func (p *Period) Exhausted() bool {
	return p.Used >= p.Allowance
}

// Left is how many emails the Period can still take.
func (p *Period) Left() int {
	return max(p.Allowance-p.Used, 0)
}

// Crossed reports which thresholds a usage moving from before to after
// crossed, so each is announced once per Period.
func (p *Period) Crossed(before, after int) (nearLimit, exhausted bool) {
	threshold := p.Allowance * NearLimitPercent
	nearLimit = before*100 < threshold && after*100 >= threshold
	exhausted = before < p.Allowance && after >= p.Allowance

	return nearLimit, exhausted
}

// PeriodStart is the first instant of the calendar month in UTC that holds t.
func PeriodStart(t time.Time) time.Time {
	u := t.UTC()

	return time.Date(u.Year(), u.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func NextPeriodStart(t time.Time) time.Time {
	return PeriodStart(t).AddDate(0, 1, 0)
}

// PeriodLabel names a Period on the wire, "YYYY-MM".
func PeriodLabel(start time.Time) string {
	return start.UTC().Format("2006-01")
}

type ChangeKind int

const (
	ChangeNothing ChangeKind = iota
	// ChangeStart is a first Package: it starts at once.
	ChangeStart
	// ChangeUpgrade is more emails: at once, the used count carried over.
	ChangeUpgrade
	// ChangeScheduled is fewer emails or a cancellation: from the next Period,
	// because this Period's price is already owed.
	ChangeScheduled
	// ChangeUnscheduled is the current Package chosen again: the scheduled
	// change is undone.
	ChangeUnscheduled
)

// Decide is what choosing target (nil to cancel) does to a Project on current.
func Decide(current *HeldPackage, scheduled *Scheduled, target *Package) ChangeKind {
	switch {
	case current == nil && target == nil:
		return ChangeNothing
	case current == nil:
		return ChangeStart
	case target == nil:
		return ChangeScheduled
	case target.Code == current.Code && scheduled != nil:
		return ChangeUnscheduled
	case target.Code == current.Code:
		return ChangeNothing
	case target.MonthlyEmails > current.MonthlyEmails:
		return ChangeUpgrade
	default:
		return ChangeScheduled
	}
}

// Hold starts holding a catalog Package at the given instant.
func Hold(p *Package, at time.Time) *HeldPackage {
	return &HeldPackage{Code: p.Code, MonthlyEmails: p.MonthlyEmails, MonthlyPrice: p.MonthlyPrice, StartedAt: at}
}
