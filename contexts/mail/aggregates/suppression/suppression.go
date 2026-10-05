// Package suppression is the Suppression: an address a Project no longer
// sends to because mail to it bounced for good or its recipient complained.
package suppression

import (
	"time"

	"github.com/google/uuid"
)

type Reason string

const (
	ReasonBounce    Reason = "bounce"
	ReasonComplaint Reason = "complaint"
)

type Suppression struct {
	ID        uuid.UUID
	ProjectID uuid.UUID
	Address   string
	Reason    Reason
	MessageID *uuid.UUID
	CreatedAt time.Time
}

// Removable is whether a Team may remove it: a complaint is a person's
// explicit wish and only Nodera overrides it.
func (s *Suppression) Removable() bool {
	return s.Reason == ReasonBounce
}
