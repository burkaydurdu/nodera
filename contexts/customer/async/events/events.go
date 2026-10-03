// Package events holds the payloads the customer context publishes. The
// notification context declares its own copies; the two never import each
// other.
package events

import (
	"encoding/json"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
)

// Recipient is who an email goes to and in which Locale.
type Recipient struct {
	UserID   uuid.UUID `json:"user_id" validate:"required"`
	Email    string    `json:"email" validate:"required,email"`
	FullName string    `json:"full_name"`
	Locale   string    `json:"locale" validate:"required"`
}

type EmailVerificationCodeBody struct {
	Recipient
	Code string `json:"code" validate:"required"`
}

type EmailVerificationCodeEvent struct {
	Payload EmailVerificationCodeBody `json:"payload" validate:"required"`
}

type EmailAlreadyRegisteredBody struct {
	Recipient
}

type EmailAlreadyRegisteredEvent struct {
	Payload EmailAlreadyRegisteredBody `json:"payload" validate:"required"`
}

type MFAEmailCodeBody struct {
	Recipient
	Code string `json:"code" validate:"required"`
}

type MFAEmailCodeEvent struct {
	Payload MFAEmailCodeBody `json:"payload" validate:"required"`
}

type PasswordResetBody struct {
	Recipient
	ResetURL string `json:"reset_url" validate:"required,url"`
}

type PasswordResetEvent struct {
	Payload PasswordResetBody `json:"payload" validate:"required"`
}

// InvitationRecipient may name no User: an Invitation can go to an address
// nobody has signed up with yet, in which case user_id is the nil uuid and
// the Locale is the inviter's.
type InvitationRecipient struct {
	UserID   uuid.UUID `json:"user_id"`
	Email    string    `json:"email" validate:"required,email"`
	FullName string    `json:"full_name"`
	Locale   string    `json:"locale" validate:"required"`
}

// TeamInvitationBody names the Invitation and the moment of this sending, so
// a resend is told again while a redelivery is not.
type TeamInvitationBody struct {
	InvitationID uuid.UUID           `json:"invitation_id" validate:"required"`
	TeamID       uuid.UUID           `json:"team_id" validate:"required"`
	SentAt       time.Time           `json:"sent_at" validate:"required"`
	Recipient    InvitationRecipient `json:"recipient" validate:"required"`
	TeamName     string              `json:"team_name" validate:"required"`
	Role         string              `json:"role" validate:"required"`
	InviterName  string              `json:"inviter_name" validate:"required"`
	AcceptURL    string              `json:"accept_url" validate:"required,url"`
	ExpiresAt    time.Time           `json:"expires_at" validate:"required"`
}

type TeamInvitationEvent struct {
	Payload TeamInvitationBody `json:"payload" validate:"required"`
}

// Domain events: what another context needs to know a Team, a member or a
// Project exists. Consumers key on ids; the Team name and the person's
// address, name and Locale ride along so billing can invoice and write to
// them without asking the customer context back.

type TeamCreatedBody struct {
	TeamID        uuid.UUID `json:"team_id" validate:"required"`
	Name          string    `json:"name" validate:"required"`
	OwnerUserID   uuid.UUID `json:"owner_user_id" validate:"required"`
	OwnerEmail    string    `json:"owner_email" validate:"required,email"`
	OwnerFullName string    `json:"owner_full_name"`
	OwnerLocale   string    `json:"owner_locale" validate:"required"`
	CreatedAt     time.Time `json:"created_at" validate:"required"`
}

type TeamCreatedEvent struct {
	Payload TeamCreatedBody `json:"payload" validate:"required"`
}

type TeamMemberAddedBody struct {
	TeamID   uuid.UUID `json:"team_id" validate:"required"`
	UserID   uuid.UUID `json:"user_id" validate:"required"`
	Role     string    `json:"role" validate:"required"`
	Email    string    `json:"email" validate:"required,email"`
	FullName string    `json:"full_name"`
	Locale   string    `json:"locale" validate:"required"`
	AddedAt  time.Time `json:"added_at" validate:"required"`
}

type TeamMemberAddedEvent struct {
	Payload TeamMemberAddedBody `json:"payload" validate:"required"`
}

// TeamMemberRoleChangedBody names who made the change, so the member is told
// only of a change someone else made.
type TeamMemberRoleChangedBody struct {
	TeamID    uuid.UUID `json:"team_id" validate:"required"`
	UserID    uuid.UUID `json:"user_id" validate:"required"`
	Role      string    `json:"role" validate:"required"`
	ChangedAt time.Time `json:"changed_at" validate:"required"`
	ChangedBy uuid.UUID `json:"changed_by" validate:"required"`
}

type TeamMemberRoleChangedEvent struct {
	Payload TeamMemberRoleChangedBody `json:"payload" validate:"required"`
}

// TeamMemberRemovedBody names who removed the member; leaving is removing
// oneself.
type TeamMemberRemovedBody struct {
	TeamID    uuid.UUID `json:"team_id" validate:"required"`
	UserID    uuid.UUID `json:"user_id" validate:"required"`
	RemovedAt time.Time `json:"removed_at" validate:"required"`
	RemovedBy uuid.UUID `json:"removed_by" validate:"required"`
}

type TeamMemberRemovedEvent struct {
	Payload TeamMemberRemovedBody `json:"payload" validate:"required"`
}

// UserProfileUpdatedBody is a person's name and Locale as of updated_at, for
// the read models that write to them (docs/adr/0008).
type UserProfileUpdatedBody struct {
	UserID    uuid.UUID `json:"user_id" validate:"required"`
	FullName  string    `json:"full_name"`
	Locale    string    `json:"locale" validate:"required"`
	UpdatedAt time.Time `json:"updated_at" validate:"required"`
}

type UserProfileUpdatedEvent struct {
	Payload UserProfileUpdatedBody `json:"payload" validate:"required"`
}

type ProjectCreatedBody struct {
	ProjectID uuid.UUID `json:"project_id" validate:"required"`
	TeamID    uuid.UUID `json:"team_id" validate:"required"`
	CreatedAt time.Time `json:"created_at" validate:"required"`
}

type ProjectCreatedEvent struct {
	Payload ProjectCreatedBody `json:"payload" validate:"required"`
}

func (e *EmailVerificationCodeEvent) IsValid() error  { return validator.New().Struct(e) }
func (e *EmailAlreadyRegisteredEvent) IsValid() error { return validator.New().Struct(e) }
func (e *MFAEmailCodeEvent) IsValid() error           { return validator.New().Struct(e) }
func (e *PasswordResetEvent) IsValid() error          { return validator.New().Struct(e) }
func (e *TeamInvitationEvent) IsValid() error         { return validator.New().Struct(e) }
func (e *TeamCreatedEvent) IsValid() error            { return validator.New().Struct(e) }
func (e *TeamMemberAddedEvent) IsValid() error        { return validator.New().Struct(e) }
func (e *TeamMemberRoleChangedEvent) IsValid() error  { return validator.New().Struct(e) }
func (e *TeamMemberRemovedEvent) IsValid() error      { return validator.New().Struct(e) }
func (e *ProjectCreatedEvent) IsValid() error         { return validator.New().Struct(e) }

func ToJSONString(event any) (string, error) {
	data, err := json.Marshal(event)
	if err != nil {
		return "", err
	}

	return string(data), nil
}
