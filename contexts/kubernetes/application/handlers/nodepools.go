package handlers

import (
	"context"
	"sort"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/kubernetes/aggregates/cluster"
	"github.com/piggystats/nodera/contexts/kubernetes/application/dto"
	"github.com/piggystats/nodera/pkg/authz"
)

// NodePools adds, resizes and removes a Cluster's Node pools and lists its
// Nodes. A pool's Plan never changes: a new pool replaces it.
type NodePools struct {
	*Clusters
}

func NewNodePools(clusters *Clusters) *NodePools {
	return &NodePools{Clusters: clusters}
}

func (h *NodePools) ListNodes(ctx context.Context, req *cluster.ListNodesRequest) ([]dto.Node, error) {
	c, err := h.auth.cluster(ctx, uuid.MustParse(req.ProjectID), uuid.MustParse(req.ClusterID), authz.KubernetesRead)
	if err != nil {
		return nil, err
	}
	nodes, err := h.clusters.ListNodes(ctx, c.ID)
	if err != nil {
		return nil, err
	}

	return dto.ToNodes(nodes), nil
}

// Add opens a pool and asks for its Nodes at once when the Control plane can
// be joined; otherwise the provisioner asks once it can.
func (h *NodePools) Add(ctx context.Context, req *cluster.AddNodePoolRequest) (*dto.Cluster, error) {
	projectID, clusterID := uuid.MustParse(req.ProjectID), uuid.MustParse(req.ClusterID)
	c, err := h.auth.cluster(ctx, projectID, clusterID, authz.KubernetesWrite)
	if err != nil {
		return nil, err
	}
	if err := refuseUnlessAlive(c); err != nil {
		return nil, err
	}
	pool, err := h.validatePool(ctx, &req.NodePoolInput, c.Region)
	if err != nil {
		return nil, err
	}
	if err := requireActiveBilling(ctx, h.billing, c.TeamID); err != nil {
		return nil, err
	}
	pools, err := h.clusters.ListPools(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	if len(pools) >= h.settings.PoolsPerCluster {
		return nil, cluster.ErrorTooManyPools
	}

	var placed []*cluster.Node
	pool.ClusterID, pool.CreatedAt = c.ID, h.now()
	c, err = h.touch(ctx, c.ID, func(ctx context.Context, locked *cluster.Cluster) error {
		if err := refuseUnlessAlive(locked); err != nil {
			return err
		}
		if err := h.clusters.CreatePool(ctx, pool); err != nil {
			return err
		}
		placed, err = h.orders.place(ctx, locked, pool, pool.Count)

		return err
	})
	if err != nil {
		return nil, err
	}
	if c.ControlPlaneReady() {
		h.orders.requestAll(ctx, c, pool, placed)
	}

	return h.show(ctx, c)
}

// Resize asks for more Nodes or releases the newest; what a Node counts for
// is its status, so a failed Node leaves a place to fill.
func (h *NodePools) Resize(ctx context.Context, req *cluster.ResizeNodePoolRequest) (*dto.Cluster, error) {
	projectID, clusterID, poolID := uuid.MustParse(req.ProjectID), uuid.MustParse(req.ClusterID), uuid.MustParse(req.PoolID)
	c, err := h.auth.cluster(ctx, projectID, clusterID, authz.KubernetesWrite)
	if err != nil {
		return nil, err
	}
	if err := refuseUnlessAlive(c); err != nil {
		return nil, err
	}
	if req.Count < cluster.MinNodes || req.Count > h.settings.NodesPerPool {
		return nil, cluster.ErrorTooManyNodes
	}

	var (
		pool   *cluster.NodePool
		placed []*cluster.Node
	)
	c, err = h.touch(ctx, c.ID, func(ctx context.Context, locked *cluster.Cluster) error {
		if err := refuseUnlessAlive(locked); err != nil {
			return err
		}
		pool, err = h.clusters.FindPoolForUpdate(ctx, locked.ID, poolID)
		if err != nil {
			return err
		}
		nodes, err := h.clusters.ListNodesByPool(ctx, pool.ID)
		if err != nil {
			return err
		}
		now := h.now()
		counting := make([]*cluster.Node, 0, len(nodes))
		for _, n := range nodes {
			switch {
			case n.Status.Counts():
				counting = append(counting, n)
			case n.Status == cluster.NodeFailed && !n.HasDroplet():
				// Choosing the count again is the answer to a Node that failed
				// before it had a Droplet: it is replaced, and nothing is left to
				// give back.
				n.Status, n.GoneAt, n.UpdatedAt = cluster.NodeGone, &now, now
				if err := h.clusters.SaveNode(ctx, n); err != nil {
					return err
				}
			}
		}
		switch {
		case req.Count > len(counting):
			if err := requireActiveBilling(ctx, h.billing, locked.TeamID); err != nil {
				return err
			}
			placed, err = h.orders.place(ctx, locked, pool, req.Count-len(counting))
			if err != nil {
				return err
			}
		case req.Count < len(counting):
			// The newest go first: they carry the least. Ids are time-ordered,
			// so Nodes made in one instant still have a newest.
			sort.Slice(counting, func(i, j int) bool {
				if counting[i].CreatedAt.Equal(counting[j].CreatedAt) {
					return counting[i].ID.String() > counting[j].ID.String()
				}

				return counting[i].CreatedAt.After(counting[j].CreatedAt)
			})
			for _, n := range counting[:len(counting)-req.Count] {
				if err := markReleasing(ctx, h.clusters, n, now); err != nil {
					return err
				}
			}
		}
		pool.Count, pool.UpdatedAt = req.Count, now
		if err := h.clusters.SavePool(ctx, pool); err != nil {
			return err
		}

		return h.resettle(ctx, locked)
	})
	if err != nil {
		return nil, err
	}
	if c.ControlPlaneReady() && len(placed) > 0 {
		h.orders.requestAll(ctx, c, pool, placed)
	}

	return h.show(ctx, c)
}

// Delete releases every Node of the pool and forgets it; a Cluster keeps at
// least one pool.
func (h *NodePools) Delete(ctx context.Context, req *cluster.DeleteNodePoolRequest) (*dto.Cluster, error) {
	projectID, clusterID, poolID := uuid.MustParse(req.ProjectID), uuid.MustParse(req.ClusterID), uuid.MustParse(req.PoolID)
	c, err := h.auth.cluster(ctx, projectID, clusterID, authz.KubernetesWrite)
	if err != nil {
		return nil, err
	}
	if err := refuseUnlessAlive(c); err != nil {
		return nil, err
	}

	c, err = h.touch(ctx, c.ID, func(ctx context.Context, locked *cluster.Cluster) error {
		if err := refuseUnlessAlive(locked); err != nil {
			return err
		}
		pools, err := h.clusters.ListPools(ctx, locked.ID)
		if err != nil {
			return err
		}
		if len(pools) <= 1 {
			return cluster.ErrorLastPool
		}
		pool, err := h.clusters.FindPoolForUpdate(ctx, locked.ID, poolID)
		if err != nil {
			return err
		}
		nodes, err := h.clusters.ListNodesByPool(ctx, pool.ID)
		if err != nil {
			return err
		}
		now := h.now()
		for _, n := range nodes {
			if err := markReleasing(ctx, h.clusters, n, now); err != nil {
				return err
			}
		}
		pool.DeletedAt, pool.UpdatedAt = &now, now
		if err := h.clusters.SavePool(ctx, pool); err != nil {
			return err
		}

		return h.resettle(ctx, locked)
	})
	if err != nil {
		return nil, err
	}

	return h.show(ctx, c)
}
