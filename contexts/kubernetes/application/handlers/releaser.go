package handlers

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/kubernetes/aggregates/cluster"
	"github.com/piggystats/nodera/contexts/kubernetes/engine"
)

// Releaser is the job that finishes a Node's Release: its workloads are
// moved off, within the grace and then by force, the Cluster forgets it, and
// the droplet context is asked to delete its Droplet. A failed Node's Droplet
// is given back the same way, without a drain; a Node that has no Droplet is
// gone at once.
type Releaser struct {
	*lifecycle
}

func NewReleaser(life *lifecycle) *Releaser {
	return &Releaser{lifecycle: life}
}

// Due answers the Nodes still to be released, in order: the releasing and
// failed ones not yet handed to the droplet context, the ones with nothing to
// hand over, and the ones handed over so long ago that no Droplet will come.
func (h *Releaser) Due(ctx context.Context, limit int) ([]*cluster.Node, error) {
	list, err := h.clusters.ListNodesByStatus(ctx, []cluster.NodeStatus{cluster.NodeReleasing, cluster.NodeFailed}, limit)
	if err != nil {
		return nil, err
	}
	out := make([]*cluster.Node, 0, len(list))
	for _, n := range list {
		switch {
		case !n.HasDroplet() && n.Status == cluster.NodeReleasing:
			out = append(out, n)
		case !n.HasDroplet():
			// A failed Node without a Droplet stays failed: it is the
			// customer's to resize away or delete with the Cluster.
		case n.ReleasedAt == nil:
			out = append(out, n)
		case n.DropletID == nil && n.Status == cluster.NodeReleasing && h.now().Sub(*n.ReleasedAt) > JoinTTL:
			// Asked for and given back without the droplet context ever
			// naming a Droplet: past the join secret's life none will come.
			out = append(out, n)
		}
	}

	return out, nil
}

// Release drains and forgets the Node in the Cluster when it ever joined,
// records that it was given back and then asks the droplet context for its
// Droplet. The engine not knowing the Node, or the Cluster, is a Node that
// never joined and nothing to drain.
func (h *Releaser) Release(ctx context.Context, n *cluster.Node) error {
	c, err := h.clusters.FindByID(ctx, n.ClusterID)
	if err != nil {
		return err
	}
	if !n.HasDroplet() || n.ReleasedAt != nil {
		return h.gone(ctx, c.ID, n.ID)
	}

	// A deleting Cluster's Nodes are not drained: there is nowhere to move
	// workloads to, and the Control plane goes right after.
	if n.Status == cluster.NodeReleasing && c.Status != cluster.StatusDeleting && c.ControlPlaneReady() {
		force := n.ReleaseAskedAt != nil && h.now().Sub(*n.ReleaseAskedAt) > DrainGrace
		err := h.engine.DrainNode(ctx, c.ID, n.Hostname, force)
		switch {
		case errors.Is(err, engine.ErrDraining):
			return nil
		case errors.Is(err, engine.ErrNotFound):
		case err != nil:
			return engineError(err)
		default:
			if err := h.engine.DeleteNode(ctx, c.ID, n.Hostname); err != nil && !errors.Is(err, engine.ErrNotFound) {
				return engineError(err)
			}
		}
	}

	var at *time.Time
	_, _, err = h.update(ctx, c.ID, func(ctx context.Context, _ *cluster.Cluster) error {
		locked, err := h.clusters.FindNodeByIDForUpdate(ctx, n.ID)
		if err != nil {
			return err
		}
		if locked.ReleasedAt != nil {
			return nil
		}
		now := h.now()
		locked.ReleasedAt, locked.UpdatedAt, at = &now, now, &now

		return h.clusters.SaveNode(ctx, locked)
	})
	if err != nil || at == nil {
		return err
	}
	// Announced after the commit, like every change (docs/adr/0001); a publish
	// that fails is logged by the caller and the Node waits for the Droplet
	// that will not come until an operator looks, which the long wait exposes.
	return h.announcer.NodeReleased(ctx, n.ID, *at)
}

// gone ends a Node that has no Droplet to wait for.
func (h *Releaser) gone(ctx context.Context, clusterID, nodeID uuid.UUID) error {
	_, err := h.touch(ctx, clusterID, func(ctx context.Context, c *cluster.Cluster) error {
		locked, err := h.clusters.FindNodeByIDForUpdate(ctx, nodeID)
		if err != nil {
			return err
		}
		if !locked.Status.Live() {
			return nil
		}
		at := h.now()
		locked.Status, locked.GoneAt, locked.UpdatedAt = cluster.NodeGone, &at, at
		if locked.ReleasedAt == nil {
			locked.ReleasedAt = &at
		}
		if err := h.clusters.SaveNode(ctx, locked); err != nil {
			return err
		}

		return h.resettle(ctx, c)
	})

	return err
}
