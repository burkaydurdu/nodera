package usage

import (
	"bytes"
	"slices"
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
// [from, to); an interval stopped at its own start never held it, and an
// empty window, such as a Period the cutover leaves nothing of, holds
// nothing.
func (i *MailInterval) Overlaps(from, to time.Time) bool {
	if !from.Before(to) || (i.StoppedAt != nil && !i.StoppedAt.After(i.StartedAt)) {
		return false
	}

	return i.StartedAt.Before(to) && (i.StoppedAt == nil || i.StoppedAt.After(from))
}

// MailLine is one Project's Mail line of an Invoice: the Package it names and
// the amount the Project still owes for the Period.
type MailLine struct {
	ProjectID   uuid.UUID
	ProjectName string
	PackageCode string
	Amount      decimal.Decimal
}

// MailLines prices the Mail of a Period from the intervals held in
// [from, to): each Project pays the highest monthly price among them, never
// prorated, less what earlier Invoices of the Period already charged for it
// (billed), so an upgrade after a Threshold Invoice costs only the
// difference. A Project with nothing more to pay has no line. The line
// follows the highest-priced interval, the latest of them on a tie, so it
// carries the Project's newest name. Projects come in the order their first
// interval in the window started.
func MailLines(intervals []*MailInterval, from, to time.Time, billed map[uuid.UUID]decimal.Decimal) []MailLine {
	type held struct {
		first   time.Time
		highest *MailInterval
	}
	projects := make(map[uuid.UUID]*held)
	for _, i := range intervals {
		if !i.Overlaps(from, to) {
			continue
		}
		p, seen := projects[i.ProjectID]
		if !seen {
			projects[i.ProjectID] = &held{first: i.StartedAt, highest: i}

			continue
		}
		if i.StartedAt.Before(p.first) {
			p.first = i.StartedAt
		}
		if outranks(i, p.highest) {
			p.highest = i
		}
	}

	ids := make([]uuid.UUID, 0, len(projects))
	for id := range projects {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b uuid.UUID) int {
		if c := projects[a].first.Compare(projects[b].first); c != 0 {
			return c
		}

		return bytes.Compare(a[:], b[:])
	})

	lines := make([]MailLine, 0, len(ids))
	for _, id := range ids {
		highest := projects[id].highest
		amount := highest.MonthlyPrice.Sub(billed[id]).RoundCeil(2)
		if !amount.IsPositive() {
			continue
		}
		lines = append(lines, MailLine{
			ProjectID: id, ProjectName: highest.ProjectName, PackageCode: highest.PackageCode, Amount: amount,
		})
	}

	return lines
}

// outranks reports whether a, rather than b, is the interval a Project's
// line follows: the higher price, or the later start at the same price.
func outranks(a, b *MailInterval) bool {
	if c := a.MonthlyPrice.Cmp(b.MonthlyPrice); c != 0 {
		return c > 0
	}

	return a.StartedAt.After(b.StartedAt)
}
