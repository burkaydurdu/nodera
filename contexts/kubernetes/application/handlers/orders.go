package handlers

import (
	"context"
	"fmt"

	"github.com/piggystats/nodera/contexts/kubernetes/aggregates/cluster"
	"github.com/piggystats/nodera/contexts/kubernetes/async/constants"
	"github.com/piggystats/nodera/logger"
)

// orderer asks the droplet context for Nodes (docs/adr/0014). A Node row is
// made first, so the pool's count is kept whatever the engine says; the
// request itself goes out only once the Control plane can be joined, from the
// handler when it already can and from the provisioner otherwise.
type orderer struct {
	*lifecycle
	userData UserDataStore
	settings Settings
}

func newOrderer(life *lifecycle, userData UserDataStore, settings Settings) *orderer {
	return &orderer{lifecycle: life, userData: userData, settings: settings}
}

// place makes count Nodes for the pool, unrequested.
func (o *orderer) place(ctx context.Context, c *cluster.Cluster, pool *cluster.NodePool, count int) ([]*cluster.Node, error) {
	nodes := make([]*cluster.Node, 0, count)
	for i := 0; i < count; i++ {
		n := &cluster.Node{
			ID: cluster.NewID(), ClusterID: c.ID, PoolID: pool.ID, Hostname: cluster.NewHostname(pool.Name),
			Status: cluster.NodeRequested, CreatedAt: o.now(),
		}
		if err := o.clusters.CreateNode(ctx, n); err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}

	return nodes, nil
}

// request makes the Node's join secret, leaves its cloud-init in Redis for the
// droplet context, publishes the request and records that it went. A failure
// before the publish leaves the Node for the provisioner to request later; a
// redelivered request is harmless to the droplet context.
func (o *orderer) request(ctx context.Context, c *cluster.Cluster, pool *cluster.NodePool, n *cluster.Node) error {
	join, err := o.engine.CreateJoin(ctx, c.ID, JoinTTL)
	if err != nil {
		return engineError(err)
	}
	userData := CloudInit(n, pool, c.Version, join)
	if err := o.userData.Set(ctx, fmt.Sprintf(constants.UserDataKeyFormat, n.ID), userData, JoinTTL); err != nil {
		return err
	}

	at := o.now()
	if err := o.announcer.NodeRequested(ctx, c, pool, n, o.settings.NodeImage, at); err != nil {
		return err
	}
	n.RequestedAt, n.UpdatedAt = &at, at

	return o.clusters.SaveNode(ctx, n)
}

// requestAll requests every Node given; a failure is logged and the Node left
// for the provisioner.
func (o *orderer) requestAll(ctx context.Context, c *cluster.Cluster, pool *cluster.NodePool, nodes []*cluster.Node) {
	for _, n := range nodes {
		if err := o.request(ctx, c, pool, n); err != nil {
			o.log.ErrorWithFields(err, logger.NewFields().Add("cluster_id", c.ID).Add("node_id", n.ID))
		}
	}
}
