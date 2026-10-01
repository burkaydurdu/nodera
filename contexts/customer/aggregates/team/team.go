package team

import (
	"time"

	"github.com/google/uuid"
)

// Role is what a member may do inside one Team. Today only owner.
type Role string

const RoleOwner Role = "owner"

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
