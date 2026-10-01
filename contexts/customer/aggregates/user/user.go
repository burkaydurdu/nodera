package user

import (
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/customer/aggregates/user/mfa"
)

// Status is a User's lifecycle state, persisted in users.status.
type Status string

const (
	StatusPendingVerification Status = "pending_verification"
	StatusActive              Status = "active"
	StatusSuspended           Status = "suspended"
)

// Locale is the language a User reads Nodera and its emails in.
type Locale string

const (
	LocaleTR Locale = "tr"
	LocaleEN Locale = "en"
)

func (l Locale) IsValid() bool {
	return l == LocaleTR || l == LocaleEN
}

type User struct {
	ID              uuid.UUID
	Email           string
	Password        string
	FullName        string
	Locale          Locale
	Country         string
	Status          Status
	MFAMethod       mfa.Method
	EmailVerifiedAt *time.Time
	// PendingInvitationID is the Invitation a sign-up came from; email
	// verification accepts it and clears this.
	PendingInvitationID *uuid.UUID
	CreatedAt           time.Time
	UpdatedAt           time.Time
	DeletedAt           *time.Time
}

func (u *User) IsActive() bool {
	return u.Status == StatusActive
}

func (u *User) IsPendingVerification() bool {
	return u.Status == StatusPendingVerification
}

// MFAEnabled reports whether sign-in needs a second factor.
func (u *User) MFAEnabled() bool {
	return u.MFAMethod != mfa.MethodNone && u.MFAMethod.IsValid()
}
