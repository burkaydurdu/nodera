package handlers

import (
	"context"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/notification/aggregates/notification"
	"github.com/piggystats/nodera/utils/str"
)

// Droplet statuses this context reacts to, as the droplet context names them.
const (
	dropletProvisioning = "provisioning"
	dropletActive       = "active"
	dropletFailed       = "failed"

	// unknownFailure words a failure from a producer that sent no code.
	unknownFailure = "unavailable"
)

// DropletStatusChange is one move of a Droplet's status; From is nil for a
// Droplet just created.
type DropletStatusChange struct {
	DropletID   uuid.UUID
	TeamID      uuid.UUID
	ProjectID   uuid.UUID
	Hostname    string
	IPv4        string
	From        *string
	Status      string
	FailureCode string
}

//go:generate mockgen -destination=../../mock/handlers/team_notifier.go -package=mockhandlers github.com/piggystats/nodera/contexts/notification/application/handlers ITeamNotifier
type ITeamNotifier interface {
	NotifyTeam(ctx context.Context, event TeamEvent) error
}

// Droplets decides which moves of a Droplet are worth telling the Team about:
// the first time it is ready, and when it fails. Every other move only
// refreshes the web app, which the droplet context does itself.
type Droplets struct {
	notifier ITeamNotifier
}

func NewDroplets(notifier ITeamNotifier) *Droplets {
	return &Droplets{notifier: notifier}
}

func (h *Droplets) StatusChanged(ctx context.Context, change DropletStatusChange) error {
	switch {
	case change.Status == dropletActive && change.From != nil && *change.From == dropletProvisioning:
		// A Droplet leaves provisioning once, so its id names the event.
		return h.notifier.NotifyTeam(ctx, TeamEvent{
			SourceKey: string(notification.TypeDropletActive) + ":" + change.DropletID.String(),
			Type:      notification.TypeDropletActive,
			TeamID:    change.TeamID,
			Params: map[string]any{
				"droplet_id": change.DropletID.String(), "project_id": change.ProjectID.String(),
				"hostname": change.Hostname, "ipv4": change.IPv4,
			},
		})
	case change.Status == dropletFailed:
		code := change.FailureCode
		if str.IsBlank(code) {
			code = unknownFailure
		}

		// The reason travels as a code: the web app and the email word it in
		// the reader's language.
		return h.notifier.NotifyTeam(ctx, TeamEvent{
			SourceKey: string(notification.TypeDropletFailed) + ":" + change.DropletID.String(),
			Type:      notification.TypeDropletFailed,
			TeamID:    change.TeamID,
			Params: map[string]any{
				"droplet_id": change.DropletID.String(), "project_id": change.ProjectID.String(),
				"hostname": change.Hostname, "failure_code": code,
			},
			EmailParams: map[string]any{"Hostname": change.Hostname, "FailureKey": "droplet_failure_" + code},
		})
	default:
		return nil
	}
}
