package handlers

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/kubernetes/aggregates/cluster"
	"github.com/piggystats/nodera/utils/str"
)

// Droplet statuses this context reacts to, as the droplet context names them.
const (
	dropletActive  = "active"
	dropletFailed  = "failed"
	dropletDeleted = "deleted"
)

// DropletChange is one move of a Node's Droplet, as the droplet context
// announced it (docs/adr/0014).
type DropletChange struct {
	NodeID        uuid.UUID
	DropletID     uuid.UUID
	Status        string
	IPv4          string
	FailureCode   string
	FailureReason string
	ChangedAt     time.Time
}

// Nodes follows the Nodes' Droplets through the droplet context's events: an
// active Droplet is a Node joining, a failed one a Node that failed, a deleted
// one a Node that is gone.
type Nodes struct {
	*lifecycle
}

func NewNodes(life *lifecycle) *Nodes {
	return &Nodes{lifecycle: life}
}

// DropletChanged applies what the droplet context said. Every write is
// idempotent, so a redelivered event changes nothing the second time; a Node
// this context does not know is acknowledged, since the Cluster may have been
// deleted meanwhile.
func (h *Nodes) DropletChanged(ctx context.Context, change DropletChange) error {
	n, err := h.clusters.FindNodeByID(ctx, change.NodeID)
	if errors.Is(err, cluster.ErrorNotFound) {
		return nil
	}
	if err != nil {
		return err
	}

	_, err = h.touch(ctx, n.ClusterID, func(ctx context.Context, c *cluster.Cluster) error {
		n, err := h.clusters.FindNodeByIDForUpdate(ctx, change.NodeID)
		if err != nil {
			return err
		}
		now := h.now()
		dropletID := change.DropletID
		n.DropletID, n.DropletStatus, n.UpdatedAt = &dropletID, change.Status, now
		if str.IsNotEmpty(change.IPv4) {
			ipv4 := change.IPv4
			n.IPv4 = &ipv4
		}
		switch change.Status {
		case dropletActive:
			if n.Status == cluster.NodeRequested {
				n.Status = cluster.NodeJoining
				at := now
				n.DropletActiveAt = &at
			}
		case dropletFailed:
			if n.Status == cluster.NodeRequested || n.Status == cluster.NodeJoining {
				code := cluster.FailureNodeRejected
				reason := change.FailureReason
				n.Status, n.FailureCode, n.FailureReason = cluster.NodeFailed, &code, &reason
			}
		case dropletDeleted:
			if n.Status.Live() {
				at := now
				n.Status, n.GoneAt = cluster.NodeGone, &at
			}
		}
		// A Droplet that turns up for a Node already given back, or given up
		// on, was opened after the release reached the droplet context: it
		// has to be given back again, or it would run and be charged for
		// nobody. The release is asked again by clearing what was recorded.
		if change.Status != dropletDeleted && (n.Status == cluster.NodeGone || (n.Status == cluster.NodeReleasing && n.ReleasedAt != nil)) {
			at := now
			n.Status, n.GoneAt, n.ReleasedAt, n.ReleaseAskedAt = cluster.NodeReleasing, nil, nil, &at
		}
		if err := h.clusters.SaveNode(ctx, n); err != nil {
			return err
		}

		return h.resettle(ctx, c)
	})

	return err
}

// Rejected is a Node the droplet context could open no Droplet for: it has
// failed with the reason the droplet context gave, or, when it was already
// being released, it has nothing to give back any more.
func (h *Nodes) Rejected(ctx context.Context, nodeID uuid.UUID, reason string) error {
	n, err := h.clusters.FindNodeByID(ctx, nodeID)
	if errors.Is(err, cluster.ErrorNotFound) {
		return nil
	}
	if err != nil {
		return err
	}

	_, err = h.touch(ctx, n.ClusterID, func(ctx context.Context, c *cluster.Cluster) error {
		n, err := h.clusters.FindNodeByIDForUpdate(ctx, nodeID)
		if err != nil {
			return err
		}
		if n.DropletID != nil || !n.Status.Live() {
			return nil
		}
		now := h.now()
		n.DropletStatus, n.UpdatedAt = cluster.DropletRejected, now
		if n.Status == cluster.NodeRequested || n.Status == cluster.NodeJoining {
			code := cluster.FailureNodeRejected
			n.Status, n.FailureCode, n.FailureReason = cluster.NodeFailed, &code, &reason
		}
		if err := h.clusters.SaveNode(ctx, n); err != nil {
			return err
		}

		return h.resettle(ctx, c)
	})

	return err
}
