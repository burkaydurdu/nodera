package usage

import (
	"bytes"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// PackageInterval is what the Period rule reads of an interval a Package was
// held for. Mail and Storage report the same thing and differ only in the
// holder their intervals are keyed by: a Project for Mail, a Storage for
// Storage.
type PackageInterval interface {
	Holder() uuid.UUID
	Price() decimal.Decimal
	// Span is [start, stop); stop is nil while the Package is still held.
	Span() (start time.Time, stop *time.Time)
}

// PackageCharge is what one holder still owes for a Period, with the
// interval its Invoice line is worded from.
type PackageCharge[I PackageInterval] struct {
	Interval I
	Amount   decimal.Decimal
}

// PackageCharges prices the Packages of a Period from the intervals held in
// [from, to): each holder pays the highest monthly price among them, never
// prorated, less what earlier Invoices of the Period already charged for it
// (billed), so an upgrade after a Threshold Invoice costs only the
// difference. A holder with nothing more to pay has no charge. The charge
// follows the highest-priced interval, the latest of them on a tie, so it
// carries the newest names. Holders come in the order their first interval
// in the window started.
func PackageCharges[I PackageInterval](intervals []I, from, to time.Time, billed map[uuid.UUID]decimal.Decimal) []PackageCharge[I] {
	type held struct {
		first   time.Time
		highest I
	}
	holders := make(map[uuid.UUID]*held)
	for _, i := range intervals {
		start, stop := i.Span()
		if !heldIn(start, stop, from, to) {
			continue
		}
		h, seen := holders[i.Holder()]
		if !seen {
			holders[i.Holder()] = &held{first: start, highest: i}

			continue
		}
		if start.Before(h.first) {
			h.first = start
		}
		if outranks(i, h.highest) {
			h.highest = i
		}
	}

	ids := make([]uuid.UUID, 0, len(holders))
	for id := range holders {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b uuid.UUID) int {
		if c := holders[a].first.Compare(holders[b].first); c != 0 {
			return c
		}

		return bytes.Compare(a[:], b[:])
	})

	charges := make([]PackageCharge[I], 0, len(ids))
	for _, id := range ids {
		highest := holders[id].highest
		amount := highest.Price().Sub(billed[id]).RoundCeil(2)
		if !amount.IsPositive() {
			continue
		}
		charges = append(charges, PackageCharge[I]{Interval: highest, Amount: amount})
	}

	return charges
}

// heldIn reports whether a Package held over [start, stop) was held at some
// instant of [from, to); an interval stopped at its own start never held it,
// and an empty window, such as a Period the cutover leaves nothing of, holds
// nothing.
func heldIn(start time.Time, stop *time.Time, from, to time.Time) bool {
	if !from.Before(to) || (stop != nil && !stop.After(start)) {
		return false
	}

	return start.Before(to) && (stop == nil || stop.After(from))
}

// outranks reports whether a, rather than b, is the interval a holder's
// charge follows: the higher price, or the later start at the same price.
func outranks(a, b PackageInterval) bool {
	if c := a.Price().Cmp(b.Price()); c != 0 {
		return c > 0
	}
	aStart, _ := a.Span()
	bStart, _ := b.Span()

	return aStart.After(bStart)
}
