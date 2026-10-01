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

// Member is a membership seen from the Team: who holds which Role since when.
type Member struct {
	TeamID    uuid.UUID
	UserID    uuid.UUID
	Role      Role
	CreatedAt time.Time
}

type Members []*Member

// MemberDetail is a Member with what the members list shows about the person.
type MemberDetail struct {
	Member
	FullName string
	Email    string
}

type MemberDetails []*MemberDetail
