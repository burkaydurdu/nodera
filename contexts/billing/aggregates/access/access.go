// Package access is the Team read model (docs/adr/0004): the Teams billing
// invoices and who is in them with which Role, learned from the customer
// context's events and never queried from it. It authorizes billing.*; who is
// told about the money is the notification context's decision (docs/adr/0008).
package access

import (
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/pkg/authz"
)

type Team struct {
	TeamID    uuid.UUID
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Member is one membership as the customer context reported it. The Role is
// stored verbatim: a value this version does not know holds no Permission
// (pkg/authz fails closed). UpdatedAt is the time of the event that last
// shaped the row, so an older event arriving later is ignored; RemovedAt set
// makes the row a tombstone, kept so a late TeamMemberAdded cannot bring the
// member back.
type Member struct {
	TeamID    uuid.UUID
	UserID    uuid.UUID
	Role      authz.Role
	CreatedAt time.Time
	UpdatedAt time.Time
	RemovedAt *time.Time
}

// Membership is what a request needs to be authorized: the caller's Role in
// the Team.
type Membership struct {
	TeamID uuid.UUID
	UserID uuid.UUID
	Role   authz.Role
}

// Allows reports whether the member's Role holds the Permission.
func (m *Membership) Allows(permission authz.Permission) bool {
	return authz.Allows(m.Role, permission)
}
