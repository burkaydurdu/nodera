// Package billingstatus is whether a Team may spend: no_payment_method,
// active or suspended. Payment methods move it between the first two;
// collection moves it to and from suspended. The droplet context learns every
// change from the TeamBillingStatusChanged event.
package billingstatus

import (
	"time"

	"github.com/google/uuid"
)

type Status string

const (
	NoPaymentMethod Status = "no_payment_method"
	Active          Status = "active"
	Suspended       Status = "suspended"
)

// TeamStatus is a Team's row; a Team without one is NoPaymentMethod. The
// pointer fields are collection's: the open Invoice behind a failed payment
// or suspension, when the Team was suspended, when its Droplets will be
// deleted and when the next Dunning retry runs.
type TeamStatus struct {
	TeamID        uuid.UUID
	Status        Status
	ChangedAt     time.Time
	OpenInvoiceID *uuid.UUID
	SuspendedAt   *time.Time
	DeletionAt    *time.Time
	NextRetryAt   *time.Time
	UpdatedAt     time.Time
}

// minStep keeps successive changes of one Team strictly ordered: the droplet
// context applies them last-writer-wins on changed_at, and PostgreSQL keeps
// microseconds.
const minStep = time.Microsecond

// Move changes the status at `at`, or a microsecond after the previous
// change when the clock has not moved past it, so two changes in the same
// instant still reach the droplet context in order.
func (s *TeamStatus) Move(to Status, at time.Time) {
	changedAt := at.UTC()
	if floor := s.ChangedAt.Add(minStep); changedAt.Before(floor) {
		changedAt = floor.UTC()
	}
	s.Status, s.ChangedAt = to, changedAt
}

// Default is what a Team that never saved a card looks like.
func Default(teamID uuid.UUID, at time.Time) *TeamStatus {
	return &TeamStatus{TeamID: teamID, Status: NoPaymentMethod, ChangedAt: at, UpdatedAt: at}
}
