package events

import (
	"time"

	"github.com/google/uuid"
)

// The customer context's Team and profile events, read into the membership
// read model (docs/adr/0008); a role change or a removal someone else made is
// also told to the member.

type TeamCreatedEvent struct {
	Payload struct {
		TeamID        uuid.UUID `json:"team_id"`
		Name          string    `json:"name"`
		OwnerUserID   uuid.UUID `json:"owner_user_id"`
		OwnerEmail    string    `json:"owner_email"`
		OwnerFullName string    `json:"owner_full_name"`
		OwnerLocale   string    `json:"owner_locale"`
		CreatedAt     time.Time `json:"created_at"`
	} `json:"payload"`
}

type TeamMemberAddedEvent struct {
	Payload struct {
		TeamID   uuid.UUID `json:"team_id"`
		UserID   uuid.UUID `json:"user_id"`
		Role     string    `json:"role"`
		Email    string    `json:"email"`
		FullName string    `json:"full_name"`
		Locale   string    `json:"locale"`
		AddedAt  time.Time `json:"added_at"`
	} `json:"payload"`
}

type TeamMemberRoleChangedEvent struct {
	Payload struct {
		TeamID    uuid.UUID `json:"team_id"`
		UserID    uuid.UUID `json:"user_id"`
		Role      string    `json:"role"`
		ChangedAt time.Time `json:"changed_at"`
		ChangedBy uuid.UUID `json:"changed_by"`
	} `json:"payload"`
}

type TeamMemberRemovedEvent struct {
	Payload struct {
		TeamID    uuid.UUID `json:"team_id"`
		UserID    uuid.UUID `json:"user_id"`
		RemovedAt time.Time `json:"removed_at"`
		RemovedBy uuid.UUID `json:"removed_by"`
	} `json:"payload"`
}

type UserProfileUpdatedEvent struct {
	Payload struct {
		UserID    uuid.UUID `json:"user_id"`
		FullName  string    `json:"full_name"`
		Locale    string    `json:"locale"`
		UpdatedAt time.Time `json:"updated_at"`
	} `json:"payload"`
}
