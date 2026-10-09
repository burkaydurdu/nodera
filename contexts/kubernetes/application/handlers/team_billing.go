package handlers

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/kubernetes/aggregates/billing"
	"github.com/piggystats/nodera/contexts/kubernetes/aggregates/cluster"
	"github.com/piggystats/nodera/logger"
)

// TeamBilling reacts to the billing context's events (docs/adr/0006): a
// suspended Team's Clusters are degraded and refuse changes while the droplet
// context powers their Nodes off, and on day 21 of Dunning the Team's Clusters
// are deleted with its Droplets.
type TeamBilling struct {
	*lifecycle
	billing billing.IService
}

func NewTeamBilling(life *lifecycle, billingService billing.IService) *TeamBilling {
	return &TeamBilling{lifecycle: life, billing: billingService}
}

// StatusChanged is last-writer-wins on the event's own time: an older event
// arriving late changes nothing and triggers nothing.
func (h *TeamBilling) StatusChanged(ctx context.Context, teamID uuid.UUID, status billing.Status, changedAt time.Time) error {
	applied, err := h.billing.Upsert(ctx, &billing.TeamBilling{TeamID: teamID, Status: status, ChangedAt: changedAt})
	if err != nil || !applied {
		return err
	}

	changed, err := h.clusters.SetTeamSuspended(ctx, teamID, status == billing.StatusSuspended)
	if err != nil {
		return err
	}
	var firstErr error
	for _, id := range changed {
		if _, err := h.touch(ctx, id, func(ctx context.Context, c *cluster.Cluster) error { return h.resettle(ctx, c) }); err != nil {
			h.log.ErrorWithFields(err, logger.NewFields().Add("cluster_id", id).Add("team_id", teamID))
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	return firstErr
}

// DeletionRequested deletes every Cluster of the Team the way a customer's
// delete would; the droplet context deleting the Nodes' Droplets at the same
// time is harmless, since deletion is idempotent on both sides.
func (h *TeamBilling) DeletionRequested(ctx context.Context, teamID uuid.UUID, _ time.Time) error {
	list, err := h.clusters.ListLiveByTeam(ctx, teamID)
	if err != nil {
		return err
	}

	var firstErr error
	for _, c := range list {
		if _, err := h.markDeleting(ctx, c.ID); err != nil {
			h.log.ErrorWithFields(err, logger.NewFields().Add("cluster_id", c.ID).Add("team_id", teamID))
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	return firstErr
}
