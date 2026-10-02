package handlers

import (
	"context"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/billing/aggregates/billingstatus"
	"github.com/piggystats/nodera/contexts/billing/application/dto"
	"github.com/piggystats/nodera/pkg/authz"
)

type Status struct {
	authorizer *Authorizer
	statuses   billingstatus.IService
}

func NewStatus(authorizer *Authorizer, statuses billingstatus.IService) *Status {
	return &Status{authorizer: authorizer, statuses: statuses}
}

// Get needs only team.read: every member sees why Droplet creation is
// blocked or the Team suspended. The answer carries no amounts.
func (h *Status) Get(ctx context.Context, req *billingstatus.GetRequest) (*dto.BillingStatus, error) {
	teamID := uuid.MustParse(req.TeamID)
	if _, err := h.authorizer.Authorize(ctx, teamID, authz.TeamRead); err != nil {
		return nil, err
	}

	status, err := h.statuses.Find(ctx, teamID)
	if err != nil {
		return nil, err
	}

	return dto.ToBillingStatus(status), nil
}
