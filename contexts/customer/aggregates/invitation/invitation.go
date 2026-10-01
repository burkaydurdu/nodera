// Package invitation is an owner's pending offer of a Role in a Team to an
// email address, whether or not a User exists for it yet. The token in the
// email is stored only as its hash; expiry is computed on read, never by a
// job.
package invitation

import (
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/pkg/authz"
)

type Status string

const (
	StatusPending  Status = "pending"
	StatusAccepted Status = "accepted"
	StatusDeclined Status = "declined"
	StatusRevoked  Status = "revoked"
	StatusExpired  Status = "expired"
)

const (
	// TTL is how long an offer stays open; a forgotten one closes itself.
	TTL = 7 * 24 * time.Hour
	// ResendCooldown keeps a resend button from turning into a mail cannon.
	ResendCooldown = 60 * time.Second
)

type Invitation struct {
	ID          uuid.UUID
	TeamID      uuid.UUID
	Email       string
	Role        authz.Role
	InvitedBy   uuid.UUID
	TokenHash   string
	ExpiresAt   time.Time
	Status      Status
	AcceptedBy  *uuid.UUID
	RespondedAt *time.Time
	LastSentAt  time.Time
	CreatedAt   time.Time

	// Read-side names, filled by the repository's joins: what the lists and
	// the email show without a second lookup.
	TeamName    string
	InviterName string
}

type Invitations []*Invitation

func (i *Invitation) IsExpired(now time.Time) bool {
	return !i.ExpiresAt.After(now)
}

// EffectiveStatus is what a reader sees: a pending offer past its date is
// expired whether or not a row has been told so.
func (i *Invitation) EffectiveStatus(now time.Time) Status {
	if i.Status == StatusPending && i.IsExpired(now) {
		return StatusExpired
	}

	return i.Status
}

// IsPending reports whether the offer can still be answered.
func (i *Invitation) IsPending(now time.Time) bool {
	return i.EffectiveStatus(now) == StatusPending
}

// ResendAvailableIn answers how long the cooldown still holds; zero or less
// means a resend is allowed.
func (i *Invitation) ResendAvailableIn(now time.Time) time.Duration {
	return i.LastSentAt.Add(ResendCooldown).Sub(now)
}
