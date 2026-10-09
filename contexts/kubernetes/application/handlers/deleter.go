package handlers

import (
	"context"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/kubernetes/aggregates/cluster"
)

// Deleter is the job that ends a deleting Cluster once its Nodes are gone:
// the Control plane is removed and the Cluster is deleted.
type Deleter struct {
	*lifecycle
}

func NewDeleter(life *lifecycle) *Deleter {
	return &Deleter{lifecycle: life}
}

func (h *Deleter) Due(ctx context.Context, limit int) ([]*cluster.Cluster, error) {
	return h.clusters.ListByStatus(ctx, []cluster.Status{cluster.StatusDeleting}, limit)
}

// Finish removes the Control plane when no Node of the Cluster still waits
// for its Droplet to be deleted, and marks the Cluster deleted.
func (h *Deleter) Finish(ctx context.Context, clusterID uuid.UUID) error {
	nodes, err := h.clusters.ListNodes(ctx, clusterID)
	if err != nil {
		return err
	}
	for _, n := range nodes {
		if n.Status.Live() {
			return nil
		}
	}

	c, err := h.clusters.FindByID(ctx, clusterID)
	if err != nil {
		return err
	}
	if c.ControlPlaneDeletedAt == nil {
		if err := h.engine.DeleteControlPlane(ctx, c.ID); err != nil {
			return engineError(err)
		}
	}

	_, _, err = h.update(ctx, clusterID, func(ctx context.Context, c *cluster.Cluster) error {
		if c.Status != cluster.StatusDeleting {
			return nil
		}
		now := h.now()
		if c.ControlPlaneDeletedAt == nil {
			c.ControlPlaneDeletedAt = &now
		}
		if err := c.Transition(cluster.StatusDeleted, now); err != nil {
			return err
		}

		return h.clusters.Save(ctx, c)
	})

	return err
}
