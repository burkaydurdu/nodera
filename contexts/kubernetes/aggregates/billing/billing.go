// Package billing is the Billing status read model (docs/adr/0006): what the
// billing context last said about a Team, learned from its events and never
// queried. A Team this context has not heard about has no payment method.
package billing

import (
	"time"

	"github.com/google/uuid"
)

type Status string

const (
	StatusNoPaymentMethod Status = "no_payment_method"
	StatusActive          Status = "active"
	StatusSuspended       Status = "suspended"
)

func (s Status) IsValid() bool {
	switch s {
	case StatusNoPaymentMethod, StatusActive, StatusSuspended:
		return true
	default:
		return false
	}
}

// TeamBilling is the newest status known for a Team. ChangedAt is the time
// of the event that set it, so an older event arriving later changes nothing.
type TeamBilling struct {
	TeamID    uuid.UUID
	Status    Status
	ChangedAt time.Time
}
