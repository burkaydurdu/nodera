// Package events is the notification context's own view of the customer and
// billing contexts' payloads: the same wire shape, declared here so the contexts
// never import each other.
package events

import (
	"time"

	"github.com/google/uuid"
)

type Recipient struct {
	UserID   uuid.UUID `json:"user_id"`
	Email    string    `json:"email"`
	FullName string    `json:"full_name"`
	Locale   string    `json:"locale"`
}

type EmailVerificationCodeEvent struct {
	Payload struct {
		Recipient
		Code string `json:"code"`
	} `json:"payload"`
}

type EmailAlreadyRegisteredEvent struct {
	Payload struct {
		Recipient
	} `json:"payload"`
}

type MFAEmailCodeEvent struct {
	Payload struct {
		Recipient
		Code string `json:"code"`
	} `json:"payload"`
}

type PasswordResetEvent struct {
	Payload struct {
		Recipient
		ResetURL string `json:"reset_url"`
	} `json:"payload"`
}

// TeamInvitationEvent invites an email address, not a User: the invitee may
// have no account, so UserID can be the nil uuid and FullName empty, and the
// Locale is then the inviter's.
type TeamInvitationEvent struct {
	Payload struct {
		InvitationID uuid.UUID `json:"invitation_id"`
		TeamID       uuid.UUID `json:"team_id"`
		SentAt       time.Time `json:"sent_at"`
		Recipient    Recipient `json:"recipient"`
		TeamName     string    `json:"team_name"`
		Role         string    `json:"role"`
		InviterName  string    `json:"inviter_name"`
		AcceptURL    string    `json:"accept_url"`
		ExpiresAt    time.Time `json:"expires_at"`
	} `json:"payload"`
}
