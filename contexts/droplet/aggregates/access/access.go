// Package access is the Project access read model (docs/adr/0004): which Team
// a Project belongs to and who is in that Team, learned from the customer
// context's events and never queried from it.
package access

import (
	"time"

	"github.com/google/uuid"
)

type ProjectAccess struct {
	ProjectID uuid.UUID
	TeamID    uuid.UUID
	CreatedAt time.Time
}

type TeamAccess struct {
	TeamID    uuid.UUID
	UserID    uuid.UUID
	Role      string
	CreatedAt time.Time
}
