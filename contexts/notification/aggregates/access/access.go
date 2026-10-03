// Package access is the Team read model (docs/adr/0004, docs/adr/0008): the
// Teams a Notification can be about and who is in them with which Role,
// learned from the customer context's events and never queried from it. It
// decides who is told and whose Inbox still shows a Team's Notifications.
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

// Member is one membership as the customer context reported it, with the
// contact details the email Channel needs. The Role is stored verbatim: a
// value this version does not know holds no Permission (pkg/authz fails
// closed). UpdatedAt is the time of the event that last shaped the row, so an
// older event arriving later is ignored.
type Member struct {
	TeamID    uuid.UUID
	UserID    uuid.UUID
	Role      authz.Role
	Email     string
	FullName  string
	Locale    string
	UpdatedAt time.Time
}

// Allows reports whether the member's Role holds the Permission.
func (m *Member) Allows(permission authz.Permission) bool {
	return authz.Allows(m.Role, permission)
}
