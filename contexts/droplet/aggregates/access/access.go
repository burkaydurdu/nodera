// Package access is the Project access read model (docs/adr/0004): which Team
// a Project belongs to and who is in that Team with which Role, learned from
// the customer context's events and never queried from it.
package access

import (
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/pkg/authz"
)

type ProjectAccess struct {
	ProjectID uuid.UUID
	TeamID    uuid.UUID
	CreatedAt time.Time
}

// TeamAccess is one membership as the customer context reported it. The Role
// is stored verbatim: a value this version does not know holds no Permission
// (pkg/authz fails closed), so a newer Role never widens access here.
// UpdatedAt is the time of the event that last shaped the row, so an older
// event arriving later is ignored; RemovedAt set makes the row a tombstone,
// kept so a late TeamMemberAdded cannot bring the member back.
type TeamAccess struct {
	TeamID    uuid.UUID
	UserID    uuid.UUID
	Role      authz.Role
	CreatedAt time.Time
	UpdatedAt time.Time
	RemovedAt *time.Time
}

// Membership is what a request needs to be authorized: the Team a Project
// belongs to and the caller's Role in it.
type Membership struct {
	TeamID uuid.UUID
	Role   authz.Role
}

// Allows reports whether the member's Role holds the Permission.
func (m *Membership) Allows(permission authz.Permission) bool {
	return authz.Allows(m.Role, permission)
}
