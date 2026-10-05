package handlers

import (
	"context"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/mail/aggregates/access"
	"github.com/piggystats/nodera/contexts/mail/aggregates/limits"
	"github.com/piggystats/nodera/logger"
)

// Operations is what Nodera's operators do until the Admin side exists.
type Operations struct {
	log       logger.ILog
	access    access.IService
	limits    limits.IService
	announcer Announcer
	now       Clock
}

func NewOperations(log logger.ILog, accessService access.IService, limitService limits.IService, announcer Announcer, now Clock) *Operations {
	return &Operations{log: log, access: accessService, limits: limitService, announcer: announcer, now: now}
}

// Unpause lifts a Project's Pause; it answers false when there was none.
func (h *Operations) Unpause(ctx context.Context, projectID uuid.UUID) (bool, error) {
	project, err := h.access.FindProject(ctx, projectID)
	if err != nil {
		return false, err
	}

	now := h.now()
	lifted, err := h.limits.Unpause(ctx, projectID, now)
	if err != nil || !lifted {
		return lifted, err
	}

	var out announcements
	out.add(func(ctx context.Context) error {
		return h.announcer.SendingChanged(ctx, project.TeamID, projectID, now)
	})
	out.publish(ctx, h.log)

	return true, nil
}
