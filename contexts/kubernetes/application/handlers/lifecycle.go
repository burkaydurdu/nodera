package handlers

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/kubernetes/aggregates/access"
	"github.com/piggystats/nodera/contexts/kubernetes/aggregates/cluster"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/logger"
)

// lifecycle is what every change to a Cluster's row goes through, whether a
// customer, a job or another context's event asked for it: one locked
// read-modify-write and, after the commit, the announcement of any change of
// status to the notification context and the open web apps. The handlers and
// the jobs share one.
type lifecycle struct {
	log          logger.ILog
	access       access.IService
	clusters     cluster.IService
	transactions postgresql.ITransactionManager
	announcer    Announcer
	engine       Engine
	now          Clock
}

func newLifecycle(log logger.ILog, accessService access.IService, clusterService cluster.IService, transactions postgresql.ITransactionManager, announcer Announcer, eng Engine, now Clock) *lifecycle {
	return &lifecycle{log: log, access: accessService, clusters: clusterService, transactions: transactions, announcer: announcer, engine: eng, now: now}
}

// update locks the Cluster for the transaction, lets mutate see the current
// row and persist what it decides, and announces a status that moved. It
// answers the Cluster mutate saw and whether its status moved; a move already
// told the web apps, so the caller nudges them only when nothing moved.
func (l *lifecycle) update(ctx context.Context, clusterID uuid.UUID, mutate func(ctx context.Context, c *cluster.Cluster) error) (*cluster.Cluster, bool, error) {
	txCtx, err := l.transactions.Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = l.transactions.Rollback(txCtx) }()

	c, err := l.clusters.FindByIDForUpdate(txCtx, clusterID)
	if err != nil {
		return nil, false, err
	}
	from := c.Status
	if err := mutate(txCtx, c); err != nil {
		return nil, false, err
	}
	if err := l.transactions.Commit(txCtx); err != nil {
		return nil, false, err
	}
	moved := from != c.Status
	if moved {
		l.announceStatus(ctx, c, &from)
	}

	return c, moved, nil
}

// touch is update for a change that should reach the web apps whether or not
// the status moved.
func (l *lifecycle) touch(ctx context.Context, clusterID uuid.UUID, mutate func(ctx context.Context, c *cluster.Cluster) error) (*cluster.Cluster, error) {
	c, moved, err := l.update(ctx, clusterID, mutate)
	if err != nil {
		return nil, err
	}
	if !moved {
		l.nudge(ctx, c)
	}

	return c, nil
}

// announceStatus publishes a status move after its commit; a failure is
// logged, never returned, because the move already happened.
func (l *lifecycle) announceStatus(ctx context.Context, c *cluster.Cluster, from *cluster.Status) {
	project, err := l.access.FindProject(ctx, c.ProjectID)
	if err != nil {
		l.log.ErrorWithFields(err, logger.NewFields().Add("cluster_id", c.ID))
		project = &access.ProjectAccess{ProjectID: c.ProjectID, TeamID: c.TeamID}
	}
	if err := l.announcer.ClusterStatusChanged(ctx, project, c, from, c.UpdatedAt); err != nil {
		l.log.ErrorWithFields(err, logger.NewFields().Add("cluster_id", c.ID).Add("status", string(c.Status)))
	}
}

// nudge tells the open web apps a Cluster changed in a way that is not a
// status move: a Node or a pool.
func (l *lifecycle) nudge(ctx context.Context, c *cluster.Cluster) {
	if err := l.announcer.ClusterChanged(ctx, c.TeamID, c.ProjectID, c.ID, l.now()); err != nil {
		l.log.ErrorWithFields(err, logger.NewFields().Add("cluster_id", c.ID))
	}
}

// resettle moves a live Cluster between running and degraded from what its
// Nodes are now; it runs inside the caller's locked update.
func (l *lifecycle) resettle(ctx context.Context, c *cluster.Cluster) error {
	if !c.Status.AllowsChange() {
		return nil
	}
	nodes, err := l.clusters.ListNodes(ctx, c.ID)
	if err != nil {
		return err
	}
	next := settle(c, nodes)
	if next == c.Status {
		return nil
	}
	if err := c.Transition(next, l.now()); err != nil {
		return err
	}

	return l.clusters.Save(ctx, c)
}

// settle is the status a live Cluster should have: running with one ready
// Node, degraded with none while the Control plane answers, and degraded
// while the Team is suspended, since the droplet context is powering the
// Nodes off whatever the kubelets still report.
func settle(c *cluster.Cluster, nodes []*cluster.Node) cluster.Status {
	ready, counting := 0, 0
	for _, n := range nodes {
		if n.Status == cluster.NodeReady {
			ready++
		}
		if n.Status.Counts() {
			counting++
		}
	}
	switch {
	case c.TeamSuspended && c.Status == cluster.StatusRunning:
		return cluster.StatusDegraded
	case c.TeamSuspended:
		return c.Status
	case ready > 0 && c.Status != cluster.StatusRunning && c.Status.CanTransitionTo(cluster.StatusRunning):
		return cluster.StatusRunning
	case ready == 0 && c.Status == cluster.StatusRunning:
		return cluster.StatusDegraded
	case ready == 0 && counting == 0 && c.Status == cluster.StatusProvisioning && c.ControlPlaneReady():
		// Every first Node failed: the Control plane answers to nobody.
		return cluster.StatusDegraded
	default:
		return c.Status
	}
}

// refuseUnlessAlive is the one answer for a change asked of a Cluster on its
// way out or failed.
func refuseUnlessAlive(c *cluster.Cluster) error {
	switch c.Status {
	case cluster.StatusDeleting, cluster.StatusDeleted:
		return cluster.ErrorDeleting
	case cluster.StatusFailed:
		return cluster.ErrorFailed
	default:
		return nil
	}
}

// markDeleting moves the Cluster and every live Node of it to their deleting
// statuses; the jobs do the rest. Idempotent: a Cluster already deleting is
// answered as it is.
func (l *lifecycle) markDeleting(ctx context.Context, clusterID uuid.UUID) (*cluster.Cluster, error) {
	c, _, err := l.update(ctx, clusterID, func(ctx context.Context, c *cluster.Cluster) error {
		if c.Status == cluster.StatusDeleting || c.Status == cluster.StatusDeleted {
			return nil
		}
		now := l.now()
		if err := c.Transition(cluster.StatusDeleting, now); err != nil {
			return err
		}
		if err := l.clusters.Save(ctx, c); err != nil {
			return err
		}
		nodes, err := l.clusters.ListNodes(ctx, c.ID)
		if err != nil {
			return err
		}
		for _, n := range nodes {
			if err := markReleasing(ctx, l.clusters, n, now); err != nil {
				return err
			}
		}

		return nil
	})

	return c, err
}

// markReleasing starts a Node's Release. A Node that never had a Droplet is
// gone at once.
func markReleasing(ctx context.Context, clusters cluster.IService, n *cluster.Node, now time.Time) error {
	if n.Status == cluster.NodeReleasing || n.Status == cluster.NodeGone {
		return nil
	}
	n.UpdatedAt = now
	if !n.HasDroplet() {
		n.Status, n.GoneAt = cluster.NodeGone, &now
	} else {
		at := now
		n.Status, n.ReleaseAskedAt = cluster.NodeReleasing, &at
	}

	return clusters.SaveNode(ctx, n)
}
