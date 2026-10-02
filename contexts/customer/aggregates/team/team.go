package team

import (
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/pkg/authz"
)

// Role is what a member may do inside one Team: a name from the shared matrix
// (pkg/authz, docs/adr/0005). The matrix decides what each one allows.
type Role string

const (
	RoleOwner     Role = Role(authz.RoleOwner)
	RoleDeveloper Role = Role(authz.RoleDeveloper)
	RoleFinance   Role = Role(authz.RoleFinance)
)

func (r Role) IsValid() bool {
	return authz.IsRole(string(r))
}

func (r Role) Allows(permission authz.Permission) bool {
	return authz.Allows(authz.Role(r), permission)
}

type Team struct {
	ID          uuid.UUID
	Name        string
	OwnerUserID uuid.UUID
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   *time.Time
}

// Membership is a Team seen from one of its members.
type Membership struct {
	Team Team
	Role Role
}

type Memberships []*Membership

type Teams []*Team

// Person is what the events say about a member besides their id, so a
// consumer can address them without asking the customer context back
// (docs/adr/0004). Locale is a string here because an aggregate never
// imports a sibling.
type Person struct {
	Email    string
	FullName string
	Locale   string
}

// TeamDetail is a Team with who opened it, as the replay announces them.
type TeamDetail struct {
	Team
	Owner Person
}

type TeamDetails []*TeamDetail

// Member is a membership seen from the Team: who holds which Role since when.
type Member struct {
	TeamID    uuid.UUID
	UserID    uuid.UUID
	Role      Role
	CreatedAt time.Time
}

type Members []*Member

// MemberDetail is a Member together with the person holding the seat.
type MemberDetail struct {
	Member
	Person
}

type MemberDetails []*MemberDetail
