// Package events is the droplet context's own view of the customer context's
// Team and Project payloads: the same wire shape, declared here so the two
// contexts never import each other (docs/adr/0004).
package events

import (
	"time"

	"github.com/google/uuid"
)

type TeamCreatedEvent struct {
	Payload struct {
		TeamID      uuid.UUID `json:"team_id"`
		OwnerUserID uuid.UUID `json:"owner_user_id"`
		CreatedAt   time.Time `json:"created_at"`
	} `json:"payload"`
}

type TeamMemberAddedEvent struct {
	Payload struct {
		TeamID  uuid.UUID `json:"team_id"`
		UserID  uuid.UUID `json:"user_id"`
		Role    string    `json:"role"`
		AddedAt time.Time `json:"added_at"`
	} `json:"payload"`
}

type ProjectCreatedEvent struct {
	Payload struct {
		ProjectID uuid.UUID `json:"project_id"`
		TeamID    uuid.UUID `json:"team_id"`
		CreatedAt time.Time `json:"created_at"`
	} `json:"payload"`
}
