// Package access is the Project access read model (docs/adr/0004): which Team
// a Project belongs to, its name, and who is in that Team with which Role,
// learned from the customer context's events and never queried from it.
package access

import (
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/pkg/authz"
)

// ProjectAccess carries the Project's name so Invoice lines and Notifications
// can name it without asking the customer context.
type ProjectAccess struct {
	ProjectID uuid.UUID
	TeamID    uuid.UUID
	Name      string
	CreatedAt time.Time
}

// Membership is what a request needs to be authorized: the Team a Project
// belongs to and the caller's Role in it.
type Membership struct {
	TeamID uuid.UUID
	Role   authz.Role
}

// Allows reports whether the member's Role holds the Permission. A Role this
// version does not know holds nothing (pkg/authz fails closed).
func (m *Membership) Allows(permission authz.Permission) bool {
	return authz.Allows(m.Role, permission)
}
