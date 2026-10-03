package handlers

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/notification/aggregates/access"
	"github.com/piggystats/nodera/contexts/notification/aggregates/notification"
)

//go:generate mockgen -destination=../../mock/handlers/person_notifier.go -package=mockhandlers github.com/piggystats/nodera/contexts/notification/application/handlers IPersonNotifier
type IPersonNotifier interface {
	NotifyPerson(ctx context.Context, event PersonEvent) error
}

// RoleChange is a member's new Role and who gave it.
type RoleChange struct {
	TeamID    uuid.UUID
	UserID    uuid.UUID
	Role      string
	ChangedBy uuid.UUID
	ChangedAt time.Time
}

// Removal is a member taken out of a Team and who did it; leaving is
// removing oneself.
type Removal struct {
	TeamID    uuid.UUID
	UserID    uuid.UUID
	RemovedBy uuid.UUID
	RemovedAt time.Time
}

// Teams tells a member what someone else did to their membership; what they
// did themselves is no news to them.
type Teams struct {
	access   access.IService
	notifier IPersonNotifier
}

func NewTeams(accessService access.IService, notifier IPersonNotifier) *Teams {
	return &Teams{access: accessService, notifier: notifier}
}

func (h *Teams) RoleChanged(ctx context.Context, change RoleChange) error {
	if change.ChangedBy == uuid.Nil || change.ChangedBy == change.UserID {
		return nil
	}
	name, err := h.teamName(ctx, change.TeamID)
	if err != nil {
		return err
	}

	teamID, userID := change.TeamID, change.UserID

	return h.notifier.NotifyPerson(ctx, PersonEvent{
		SourceKey: sourceKey(notification.TypeTeamRoleChanged, teamID, userID, change.ChangedAt),
		Type:      notification.TypeTeamRoleChanged,
		Recipient: notification.Recipient{UserID: &userID},
		TeamID:    &teamID,
		Params:    map[string]any{"team_id": teamID.String(), "team_name": name, "role": change.Role},
	})
}

// MemberRemoved belongs to no Team in the Inbox: the person is no longer in
// it, and the Notification must stay visible to them.
func (h *Teams) MemberRemoved(ctx context.Context, removal Removal) error {
	if removal.RemovedBy == uuid.Nil || removal.RemovedBy == removal.UserID {
		return nil
	}
	name, err := h.teamName(ctx, removal.TeamID)
	if err != nil {
		return err
	}

	userID := removal.UserID

	return h.notifier.NotifyPerson(ctx, PersonEvent{
		SourceKey: sourceKey(notification.TypeTeamMemberRemoved, removal.TeamID, userID, removal.RemovedAt),
		Type:      notification.TypeTeamMemberRemoved,
		Recipient: notification.Recipient{UserID: &userID},
		Params:    map[string]any{"team_id": removal.TeamID.String(), "team_name": name},
	})
}

func (h *Teams) teamName(ctx context.Context, teamID uuid.UUID) (string, error) {
	team, err := h.access.FindTeam(ctx, teamID)
	if err != nil || team == nil {
		return "", err
	}

	return team.Name, nil
}

// sourceKey names a membership change by its Team, member and time, so its
// redelivery is recognised.
func sourceKey(t notification.Type, teamID, userID uuid.UUID, at time.Time) string {
	return string(t) + ":" + teamID.String() + ":" + userID.String() + ":" + at.UTC().Format(time.RFC3339Nano)
}
