package handlers

import (
	"context"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/kubernetes/aggregates/cluster"
	"github.com/piggystats/nodera/contexts/kubernetes/engine"
)

// Watcher is the job that reads each live Cluster's API server for its
// Nodes and records which have joined and are ready, fails the ones that
// never became ready in time, and keeps the Cluster's status true to its
// Nodes.
type Watcher struct {
	*lifecycle
}

func NewWatcher(life *lifecycle) *Watcher {
	return &Watcher{lifecycle: life}
}

// Live answers the Clusters whose API server can be asked.
func (h *Watcher) Live(ctx context.Context, limit int) ([]*cluster.Cluster, error) {
	list, err := h.clusters.ListByStatus(ctx, []cluster.Status{cluster.StatusProvisioning, cluster.StatusRunning, cluster.StatusDegraded}, limit)
	if err != nil {
		return nil, err
	}
	out := make([]*cluster.Cluster, 0, len(list))
	for _, c := range list {
		if c.ControlPlaneReady() {
			out = append(out, c)
		}
	}

	return out, nil
}

// Watch asks the API server once and applies what it says to every Node of
// the Cluster. A Node is joining until its kubelet first reports ready, and
// one that has not within the timeout of its Droplet being active has failed,
// whether or not it registered meanwhile; a Node once ready moves between
// ready and not ready with its kubelet.
func (h *Watcher) Watch(ctx context.Context, clusterID uuid.UUID) error {
	members, err := h.engine.ListNodes(ctx, clusterID)
	if err != nil {
		return engineError(err)
	}
	byProvider := make(map[string]engine.Node, len(members))
	for _, m := range members {
		byProvider[m.ProviderID] = m
	}

	_, err = h.touch(ctx, clusterID, func(ctx context.Context, c *cluster.Cluster) error {
		if !c.Status.AllowsChange() {
			return nil
		}
		nodes, err := h.clusters.ListNodes(ctx, c.ID)
		if err != nil {
			return err
		}
		now := h.now()
		for _, n := range nodes {
			member, joined := byProvider[n.ProviderID()]
			next := n.Status
			switch n.Status {
			case cluster.NodeJoining:
				switch {
				case joined && member.Ready:
					next = cluster.NodeReady
				case n.DropletActiveAt != nil && now.Sub(*n.DropletActiveAt) > NodeJoinTimeout:
					next = cluster.NodeFailed
				}
			case cluster.NodeReady, cluster.NodeNotReady:
				next = cluster.NodeNotReady
				if joined && member.Ready {
					next = cluster.NodeReady
				}
			case cluster.NodeRequested, cluster.NodeReleasing, cluster.NodeGone, cluster.NodeFailed:
			}
			if next == n.Status {
				continue
			}
			n.Status, n.UpdatedAt = next, now
			switch next {
			case cluster.NodeReady:
				if n.ReadyAt == nil {
					at := now
					n.ReadyAt = &at
				}
			case cluster.NodeFailed:
				code := cluster.FailureTimedOut
				reason := cluster.ErrorNodeTimedOut.Message
				n.FailureCode, n.FailureReason = &code, &reason
			case cluster.NodeRequested, cluster.NodeJoining, cluster.NodeNotReady, cluster.NodeReleasing, cluster.NodeGone:
			}
			if err := h.clusters.SaveNode(ctx, n); err != nil {
				return err
			}
		}

		return h.resettle(ctx, c)
	})

	return err
}
