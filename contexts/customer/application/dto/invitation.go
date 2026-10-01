package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/customer/aggregates/invitation"
)

type InvitedBy struct {
	UserID   uuid.UUID `json:"user_id"`
	FullName string    `json:"full_name"`
}

// Invitation is the offer as its owner and its invitee see it. Status is the
// effective one: a pending offer past its date reads as expired.
type Invitation struct {
	ID          uuid.UUID  `json:"id"`
	TeamID      uuid.UUID  `json:"team_id"`
	TeamName    string     `json:"team_name"`
	Email       string     `json:"email"`
	Role        string     `json:"role"`
	InvitedBy   InvitedBy  `json:"invited_by"`
	Status      string     `json:"status"`
	ExpiresAt   time.Time  `json:"expires_at"`
	CreatedAt   time.Time  `json:"created_at"`
	RespondedAt *time.Time `json:"responded_at"`
}

func ToInvitation(inv *invitation.Invitation, now time.Time) *Invitation {
	out := &Invitation{
		ID:        inv.ID,
		TeamID:    inv.TeamID,
		TeamName:  inv.TeamName,
		Email:     inv.Email,
		Role:      string(inv.Role),
		InvitedBy: InvitedBy{UserID: inv.InvitedBy, FullName: inv.InviterName},
		Status:    string(inv.EffectiveStatus(now)),
		ExpiresAt: inv.ExpiresAt.UTC(),
		CreatedAt: inv.CreatedAt.UTC(),
	}
	if inv.RespondedAt != nil {
		responded := inv.RespondedAt.UTC()
		out.RespondedAt = &responded
	}

	return out
}

func ToInvitations(invitations invitation.Invitations, now time.Time) []*Invitation {
	out := make([]*Invitation, 0, len(invitations))
	for _, inv := range invitations {
		out = append(out, ToInvitation(inv, now))
	}

	return out
}

// InvitationLookup is what the public accept page learns from a token alone.
// The holder of the link is the invitee, so while the offer is open the
// address is theirs to see (the sign-up form locks it); once the offer is
// dead only the hint remains.
type InvitationLookup struct {
	TeamName      string    `json:"team_name"`
	Role          string    `json:"role"`
	InvitedByName string    `json:"invited_by_name"`
	Email         string    `json:"email,omitempty"`
	EmailHint     string    `json:"email_hint"`
	Status        string    `json:"status"`
	ExpiresAt     time.Time `json:"expires_at"`
}

func ToInvitationLookup(inv *invitation.Invitation, now time.Time) *InvitationLookup {
	out := &InvitationLookup{
		TeamName:      inv.TeamName,
		Role:          string(inv.Role),
		InvitedByName: inv.InviterName,
		EmailHint:     invitation.MaskEmail(inv.Email),
		Status:        string(inv.EffectiveStatus(now)),
		ExpiresAt:     inv.ExpiresAt.UTC(),
	}
	if inv.IsPending(now) {
		out.Email = inv.Email
	}

	return out
}

// AcceptedInvitation carries the Team the caller just joined, so the web app
// can select it right away.
type AcceptedInvitation struct {
	Team *Team `json:"team"`
}
