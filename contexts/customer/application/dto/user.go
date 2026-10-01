package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/customer/aggregates/session"
	"github.com/piggystats/nodera/contexts/customer/aggregates/team"
	"github.com/piggystats/nodera/contexts/customer/aggregates/user"
)

type TeamSummary struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	Role string    `json:"role"`
}

type User struct {
	ID              uuid.UUID    `json:"id"`
	Email           string       `json:"email"`
	FullName        string       `json:"full_name"`
	Locale          string       `json:"locale"`
	Country         string       `json:"country"`
	Status          string       `json:"status"`
	MFAMethod       string       `json:"mfa_method"`
	EmailVerifiedAt *time.Time   `json:"email_verified_at"`
	CreatedAt       time.Time    `json:"created_at"`
	Team            *TeamSummary `json:"team"`
}

func ToUser(u *user.User, membership *team.Membership) *User {
	out := &User{
		ID:              u.ID,
		Email:           u.Email,
		FullName:        u.FullName,
		Locale:          string(u.Locale),
		Country:         u.Country,
		Status:          string(u.Status),
		MFAMethod:       string(u.MFAMethod),
		EmailVerifiedAt: u.EmailVerifiedAt,
		CreatedAt:       u.CreatedAt,
	}
	if membership != nil {
		out.Team = &TeamSummary{ID: membership.Team.ID, Name: membership.Team.Name, Role: string(membership.Role)}
	}

	return out
}

type SessionItem struct {
	ID         uuid.UUID `json:"id"`
	IP         string    `json:"ip"`
	UserAgent  string    `json:"user_agent"`
	CreatedAt  time.Time `json:"created_at"`
	LastUsedAt time.Time `json:"last_used_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	Current    bool      `json:"current"`
}

func ToSessionItems(sessions session.Sessions, current uuid.UUID) []*SessionItem {
	items := make([]*SessionItem, 0, len(sessions))
	for _, s := range sessions {
		items = append(items, &SessionItem{
			ID:         s.ID,
			IP:         s.IP,
			UserAgent:  s.UserAgent,
			CreatedAt:  s.CreatedAt,
			LastUsedAt: s.LastUsedAt,
			ExpiresAt:  s.ExpiresAt,
			Current:    s.ID == current,
		})
	}

	return items
}
