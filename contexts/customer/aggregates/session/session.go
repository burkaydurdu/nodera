// Package session is a signed-in User on one device: an access token and the
// refresh token that renews it (docs/adr/0002). Sessions live in Redis only;
// the row is the session record keyed by its id, plus a per-user index.
package session

import (
	"time"

	"github.com/google/uuid"
)

const (
	KeyFormat        = "session:%s"
	UserIndexFormat  = "user_sessions:%s"
	TokenTypeAccess  = "access"
	TokenTypeRefresh = "refresh"
)

// RotationGrace is how long the previous refresh token stays accepted after
// a rotation, so two tabs refreshing at once do not sign each other out.
// A reuse outside the grace is treated as theft.
const RotationGrace = 10 * time.Second

type Session struct {
	ID          uuid.UUID `json:"id"`
	UserID      uuid.UUID `json:"user_id"`
	RefreshJTI  uuid.UUID `json:"refresh_jti"`
	PreviousJTI uuid.UUID `json:"previous_jti"`
	RotatedAt   time.Time `json:"rotated_at"`
	IP          string    `json:"ip"`
	UserAgent   string    `json:"user_agent"`
	CreatedAt   time.Time `json:"created_at"`
	LastUsedAt  time.Time `json:"last_used_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type Sessions []*Session

// Device describes the client a session is opened for.
type Device struct {
	IP        string
	UserAgent string
}

// Principal is what the tokens say about the User.
type Principal struct {
	UserID uuid.UUID
	Email  string
	Status string
}

// TokenPair is what a sign-in, a verification and a refresh hand back.
type TokenPair struct {
	AccessToken      string
	RefreshToken     string
	AccessExpiresAt  time.Time
	RefreshExpiresAt time.Time
}
