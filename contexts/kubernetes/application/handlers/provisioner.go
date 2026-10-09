package handlers

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/kubernetes/aggregates/cluster"
	"github.com/piggystats/nodera/contexts/kubernetes/engine"
	"github.com/piggystats/nodera/logger"
)

// Provisioner is the job that brings a provisioning Cluster's Control plane
// up: asks the engine for it, waits for it to answer, prepares it for Nodes
// and then requests the Nodes that were waiting. Every step is idempotent, so
// a run that fails anywhere is finished by the next.
type Provisioner struct {
	*lifecycle
	orders *orderer
}

func NewProvisioner(life *lifecycle, orders *orderer) *Provisioner {
	return &Provisioner{lifecycle: life, orders: orders}
}

// Pending answers the Clusters whose Control plane is not yet ready.
func (h *Provisioner) Pending(ctx context.Context, limit int) ([]*cluster.Cluster, error) {
	return h.clusters.ListByStatus(ctx, []cluster.Status{cluster.StatusProvisioning}, limit)
}

// Advance takes one Cluster one step further: created, ready, prepared. A
// Control plane pending past the timeout fails the Cluster.
func (h *Provisioner) Advance(ctx context.Context, clusterID uuid.UUID) error {
	c, err := h.clusters.FindByID(ctx, clusterID)
	if err != nil {
		return err
	}
	if c.Status != cluster.StatusProvisioning {
		return nil
	}

	if c.ControlPlaneCreatedAt == nil {
		if err := h.engine.CreateControlPlane(ctx, ControlPlaneSpec(c)); err != nil {
			return engineError(err)
		}
		if c, err = h.stamp(ctx, c.ID, func(c *cluster.Cluster) **time.Time { return &c.ControlPlaneCreatedAt }); err != nil {
			return err
		}
	}

	if c.ControlPlaneReadyAt == nil {
		status, err := h.engine.ControlPlaneStatus(ctx, c.ID)
		if err != nil {
			return engineError(err)
		}
		switch status.State {
		case engine.ControlPlaneReady:
			if c, err = h.stamp(ctx, c.ID, func(c *cluster.Cluster) **time.Time { return &c.ControlPlaneReadyAt }); err != nil {
				return err
			}
		case engine.ControlPlaneMissing:
			// Asked for and not there: ask again next run.
			return h.unstamp(ctx, c.ID)
		case engine.ControlPlanePending:
			if h.now().Sub(*c.ControlPlaneCreatedAt) > ControlPlaneTimeout {
				return h.fail(ctx, c.ID, cluster.FailureControlPlane, cluster.ErrorControlPlaneTimedOut.Message)
			}

			return nil
		}
	}

	if c.PreparedAt == nil {
		if err := h.engine.PrepareCluster(ctx, c.ID); err != nil {
			return engineError(err)
		}
		if _, err = h.stamp(ctx, c.ID, func(c *cluster.Cluster) **time.Time { return &c.PreparedAt }); err != nil {
			return err
		}
	}

	return nil
}

// stamp records a step the engine took, at the current time, once.
func (h *Provisioner) stamp(ctx context.Context, clusterID uuid.UUID, field func(*cluster.Cluster) **time.Time) (*cluster.Cluster, error) {
	c, _, err := h.update(ctx, clusterID, func(ctx context.Context, c *cluster.Cluster) error {
		at := field(c)
		if *at != nil {
			return nil
		}
		now := h.now()
		*at, c.UpdatedAt = &now, now

		return h.clusters.Save(ctx, c)
	})

	return c, err
}

func (h *Provisioner) unstamp(ctx context.Context, clusterID uuid.UUID) error {
	_, _, err := h.update(ctx, clusterID, func(ctx context.Context, c *cluster.Cluster) error {
		c.ControlPlaneCreatedAt, c.UpdatedAt = nil, h.now()

		return h.clusters.Save(ctx, c)
	})

	return err
}

func (h *Provisioner) fail(ctx context.Context, clusterID uuid.UUID, code cluster.FailureCode, reason string) error {
	_, _, err := h.update(ctx, clusterID, func(ctx context.Context, c *cluster.Cluster) error {
		if !c.Status.CanTransitionTo(cluster.StatusFailed) {
			return nil
		}
		if err := c.Fail(code, reason, h.now()); err != nil {
			return err
		}

		return h.clusters.Save(ctx, c)
	})

	return err
}

// Awaiting answers the Nodes whose Cluster can now be joined and that were
// never asked of the droplet context.
func (h *Provisioner) Awaiting(ctx context.Context, limit int) ([]*cluster.Node, error) {
	return h.clusters.ListNodesAwaitingRequest(ctx, limit)
}

// Request asks the droplet context for one waiting Node.
func (h *Provisioner) Request(ctx context.Context, n *cluster.Node) error {
	c, err := h.clusters.FindByID(ctx, n.ClusterID)
	if err != nil {
		return err
	}
	pool, err := h.clusters.FindPool(ctx, c.ID, n.PoolID)
	if errors.Is(err, cluster.ErrorPoolNotFound) {
		// The pool went while its Nodes waited: they go with it.
		_, _, err := h.update(ctx, c.ID, func(ctx context.Context, _ *cluster.Cluster) error {
			locked, err := h.clusters.FindNodeByIDForUpdate(ctx, n.ID)
			if err != nil {
				return err
			}

			return markReleasing(ctx, h.clusters, locked, h.now())
		})

		return err
	}
	if err != nil {
		return err
	}
	if err := h.orders.request(ctx, c, pool, n); err != nil {
		h.log.ErrorWithFields(err, logger.NewFields().Add("cluster_id", c.ID).Add("node_id", n.ID))

		return err
	}

	return nil
}
