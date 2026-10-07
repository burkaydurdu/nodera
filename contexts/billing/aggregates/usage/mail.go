package usage

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// MailInterval is one Package a Project held, [StartedAt, StoppedAt), as the
// mail context reports it; StoppedAt is nil while the Project holds it. The
// price was copied from the catalog when the Package was chosen, so a later
// catalog change never reprices it.
type MailInterval struct {
	ProjectID     uuid.UUID
	TeamID        uuid.UUID
	ProjectName   string
	PackageCode   string
	MonthlyEmails int
	MonthlyPrice  decimal.Decimal
	StartedAt     time.Time
	StoppedAt     *time.Time
}

// Overlaps reports whether the Project held the Package at some instant of
// [from, to).
func (i *MailInterval) Overlaps(from, to time.Time) bool {
	return heldIn(i.StartedAt, i.StoppedAt, from, to)
}

// Holder keys Mail's intervals by the Project: a Project holds one Package
// at a time.
func (i *MailInterval) Holder() uuid.UUID { return i.ProjectID }

func (i *MailInterval) Price() decimal.Decimal { return i.MonthlyPrice }

func (i *MailInterval) Span() (time.Time, *time.Time) { return i.StartedAt, i.StoppedAt }

// MailLine is one Project's Mail line of an Invoice: the Package it names and
// the amount the Project still owes for the Period.
type MailLine struct {
	ProjectID   uuid.UUID
	ProjectName string
	PackageCode string
	Amount      decimal.Decimal
}

// MailLines is the Mail of a Period: one line per Project that still owes
// for the Packages it held in [from, to) (see PackageCharges), carrying the
// Project's newest name.
func MailLines(intervals []*MailInterval, from, to time.Time, billed map[uuid.UUID]decimal.Decimal) []MailLine {
	charges := PackageCharges(intervals, from, to, billed)
	lines := make([]MailLine, 0, len(charges))
	for _, c := range charges {
		lines = append(lines, MailLine{
			ProjectID: c.Interval.ProjectID, ProjectName: c.Interval.ProjectName, PackageCode: c.Interval.PackageCode, Amount: c.Amount,
		})
	}

	return lines
}
