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

type TeamInvitationBody struct {
	Recipient   InvitationRecipient `json:"recipient" validate:"required"`
	TeamName    string              `json:"team_name" validate:"required"`
	Role        string              `json:"role" validate:"required"`
	InviterName string              `json:"inviter_name" validate:"required"`
	AcceptURL   string              `json:"accept_url" validate:"required,url"`
	ExpiresAt   time.Time           `json:"expires_at" validate:"required"`
}

type TeamInvitationEvent struct {
	Payload TeamInvitationBody `json:"payload" validate:"required"`
}

// Domain events: what another context needs to know a Team, a member or a
// Project exists. Names never travel; the consumer keys on ids only.

type TeamCreatedBody struct {
	TeamID      uuid.UUID `json:"team_id" validate:"required"`
	OwnerUserID uuid.UUID `json:"owner_user_id" validate:"required"`
	CreatedAt   time.Time `json:"created_at" validate:"required"`
}

type TeamCreatedEvent struct {
	Payload TeamCreatedBody `json:"payload" validate:"required"`
}

type TeamMemberAddedBody struct {
	TeamID  uuid.UUID `json:"team_id" validate:"required"`
	UserID  uuid.UUID `json:"user_id" validate:"required"`
	Role    string    `json:"role" validate:"required"`
	AddedAt time.Time `json:"added_at" validate:"required"`
}

type TeamMemberAddedEvent struct {
	Payload TeamMemberAddedBody `json:"payload" validate:"required"`
}

type TeamMemberRoleChangedBody struct {
	TeamID    uuid.UUID `json:"team_id" validate:"required"`
	UserID    uuid.UUID `json:"user_id" validate:"required"`
	Role      string    `json:"role" validate:"required"`
	ChangedAt time.Time `json:"changed_at" validate:"required"`
}

type TeamMemberRoleChangedEvent struct {
	Payload TeamMemberRoleChangedBody `json:"payload" validate:"required"`
}

type TeamMemberRemovedBody struct {
	TeamID    uuid.UUID `json:"team_id" validate:"required"`
	UserID    uuid.UUID `json:"user_id" validate:"required"`
	RemovedAt time.Time `json:"removed_at" validate:"required"`
}

type TeamMemberRemovedEvent struct {
	Payload TeamMemberRemovedBody `json:"payload" validate:"required"`
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
