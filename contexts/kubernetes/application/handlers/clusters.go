package handlers

import (
	"context"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/kubernetes/aggregates/billing"
	"github.com/piggystats/nodera/contexts/kubernetes/aggregates/catalog"
	"github.com/piggystats/nodera/contexts/kubernetes/aggregates/cluster"
	"github.com/piggystats/nodera/contexts/kubernetes/application/dto"
	"github.com/piggystats/nodera/contexts/kubernetes/engine"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/authz"
	"github.com/piggystats/nodera/pkg/contextutil"
)

// Clusters opens, shows, renames and deletes a Project's Clusters and cuts
// Kubeconfigs.
type Clusters struct {
	*lifecycle
	auth     authorizer
	billing  billing.IService
	catalog  catalog.IService
	orders   *orderer
	settings Settings
}

func NewClusters(life *lifecycle, billingService billing.IService, catalogService catalog.IService, orders *orderer, settings Settings) *Clusters {
	return &Clusters{
		lifecycle: life,
		auth:      authorizer{access: life.access, clusters: life.clusters},
		billing:   billingService, catalog: catalogService, orders: orders, settings: settings,
	}
}

func (h *Clusters) List(ctx context.Context, req *cluster.ListRequest) ([]*dto.Cluster, error) {
	projectID := uuid.MustParse(req.ProjectID)
	membership, err := h.auth.project(ctx, projectID, authz.KubernetesRead)
	if err != nil {
		return nil, err
	}
	list, err := h.clusters.ListByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	status, err := h.billing.StatusOf(ctx, membership.TeamID)
	if err != nil {
		return nil, err
	}

	out := make([]*dto.Cluster, 0, len(list))
	for _, c := range list {
		pools, err := h.clusters.ListPools(ctx, c.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, dto.ToCluster(c, pools, string(status)))
	}

	return out, nil
}

func (h *Clusters) Get(ctx context.Context, req *cluster.GetRequest) (*dto.Cluster, error) {
	c, err := h.auth.cluster(ctx, uuid.MustParse(req.ProjectID), uuid.MustParse(req.ClusterID), authz.KubernetesRead)
	if err != nil {
		return nil, err
	}

	return h.show(ctx, c)
}

func (h *Clusters) show(ctx context.Context, c *cluster.Cluster) (*dto.Cluster, error) {
	pools, err := h.clusters.ListPools(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	status, err := h.billing.StatusOf(ctx, c.TeamID)
	if err != nil {
		return nil, err
	}

	return dto.ToCluster(c, pools, string(status)), nil
}

// Open records the Cluster, its first Node pool and that pool's Nodes, and
// then asks the engine for the Control plane. The answer is the provisioning
// Cluster whatever the engine says: the provisioner asks again, and the
// outcome arrives through list and detail.
func (h *Clusters) Open(ctx context.Context, req *cluster.OpenRequest) (*dto.Cluster, error) {
	projectID := uuid.MustParse(req.ProjectID)
	membership, err := h.auth.project(ctx, projectID, authz.KubernetesWrite)
	if err != nil {
		return nil, err
	}
	req.Normalize()
	if !cluster.ValidName(req.Name, cluster.MaxNameLength) {
		return nil, cluster.ErrorInvalidName
	}
	region, err := h.catalog.FindRegion(ctx, req.Region)
	if err != nil {
		return nil, err
	}
	if region.Code != h.settings.Region {
		return nil, catalog.ErrorRegionNotSold
	}
	version, err := h.catalog.FindOfferedVersion(ctx, req.Version)
	if err != nil {
		return nil, err
	}
	pool, err := h.validatePool(ctx, &req.NodePool, region.Code)
	if err != nil {
		return nil, err
	}
	if err := requireActiveBilling(ctx, h.billing, membership.TeamID); err != nil {
		return nil, err
	}
	count, err := h.clusters.CountByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if count >= h.settings.ClustersPerProject {
		return nil, cluster.ErrorTooManyClusters
	}
	project, err := h.access.FindProject(ctx, projectID)
	if err != nil {
		return nil, err
	}

	now := h.now()
	c := &cluster.Cluster{
		ID: cluster.NewID(), ProjectID: projectID, TeamID: membership.TeamID, Region: region.Code, Name: req.Name, Version: version.Version,
		Status: cluster.StatusProvisioning, CreatedBy: contextutil.CurrentUserID(ctx), CreatedAt: now, UpdatedAt: now,
	}
	c.Endpoint = h.settings.Endpoint(c.ID, c.Region)
	pool.ClusterID, pool.CreatedAt = c.ID, now

	txCtx, err := h.transactions.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = h.transactions.Rollback(txCtx) }()
	if c.EndpointPort, err = h.clusters.NextEndpointPort(txCtx, c.Region, h.settings.FirstEndpointPort, h.settings.LastEndpointPort); err != nil {
		return nil, err
	}
	if err := h.clusters.Create(txCtx, c); err != nil {
		return nil, err
	}
	if err := h.clusters.CreatePool(txCtx, pool); err != nil {
		return nil, err
	}
	if _, err := h.orders.place(txCtx, c, pool, pool.Count); err != nil {
		return nil, err
	}
	if err := h.transactions.Commit(txCtx); err != nil {
		return nil, err
	}
	if err := h.announcer.ClusterStatusChanged(ctx, project, c, nil, now); err != nil {
		h.log.ErrorWithFields(err, logger.NewFields().Add("cluster_id", c.ID))
	}

	h.follow(ctx, c)

	return h.show(ctx, c)
}

// follow asks the engine for the Control plane right after the commit and
// records that it was asked; a failure is the provisioner's to repeat.
func (h *Clusters) follow(ctx context.Context, c *cluster.Cluster) {
	if err := h.engine.CreateControlPlane(ctx, ControlPlaneSpec(c)); err != nil {
		h.log.ErrorWithFields(engineError(err), logger.NewFields().Add("cluster_id", c.ID))

		return
	}
	_, _, err := h.update(ctx, c.ID, func(ctx context.Context, locked *cluster.Cluster) error {
		if locked.ControlPlaneCreatedAt != nil {
			return nil
		}
		at := h.now()
		locked.ControlPlaneCreatedAt, locked.UpdatedAt = &at, at

		return h.clusters.Save(ctx, locked)
	})
	if err != nil {
		h.log.ErrorWithFields(err, logger.NewFields().Add("cluster_id", c.ID))
	}
}

// validatePool checks a Node pool as asked for against the catalog and the
// limits and answers it ready to store, without a Cluster yet.
func (h *Clusters) validatePool(ctx context.Context, in *cluster.NodePoolInput, region string) (*cluster.NodePool, error) {
	in.Normalize()
	if !cluster.ValidName(in.Name, cluster.MaxPoolNameLength) {
		return nil, cluster.ErrorInvalidPoolName
	}
	if in.Count < cluster.MinNodes || in.Count > h.settings.NodesPerPool {
		return nil, cluster.ErrorTooManyNodes
	}
	if err := cluster.ValidateLabels(in.Labels); err != nil {
		return nil, err
	}
	if err := cluster.ValidateTaints(in.Taints); err != nil {
		return nil, err
	}
	plan, err := h.catalog.FindPlan(ctx, in.Plan, region)
	if err != nil {
		return nil, err
	}
	if !plan.FitsNode() {
		return nil, catalog.ErrorPlanNotForNodes
	}
	if plan.SoldOut {
		return nil, catalog.ErrorPlanSoldOut
	}

	return &cluster.NodePool{ID: cluster.NewID(), Name: in.Name, PlanCode: plan.Code, Count: in.Count, Labels: in.Labels, Taints: in.Taints}, nil
}

// Rename gives the Cluster another name; the Endpoint carries the id, so
// nothing else changes.
func (h *Clusters) Rename(ctx context.Context, req *cluster.RenameRequest) (*dto.Cluster, error) {
	projectID, clusterID := uuid.MustParse(req.ProjectID), uuid.MustParse(req.ClusterID)
	if _, err := h.auth.cluster(ctx, projectID, clusterID, authz.KubernetesWrite); err != nil {
		return nil, err
	}
	req.Normalize()
	if !cluster.ValidName(req.Name, cluster.MaxNameLength) {
		return nil, cluster.ErrorInvalidName
	}

	c, err := h.touch(ctx, clusterID, func(ctx context.Context, c *cluster.Cluster) error {
		if err := refuseUnlessAlive(c); err != nil {
			return err
		}
		if c.Name == req.Name {
			return nil
		}
		c.Name, c.UpdatedAt = req.Name, h.now()

		return h.clusters.Save(ctx, c)
	})
	if err != nil {
		return nil, err
	}

	return h.show(ctx, c)
}

// Delete starts the Cluster's deletion: its Nodes are released and the
// Control plane removed by the jobs, and the Cluster is shown as deleting
// until then. Allowed whatever the Team's Billing status.
func (h *Clusters) Delete(ctx context.Context, req *cluster.DeleteRequest) (*dto.Cluster, error) {
	projectID, clusterID := uuid.MustParse(req.ProjectID), uuid.MustParse(req.ClusterID)
	if _, err := h.auth.cluster(ctx, projectID, clusterID, authz.KubernetesWrite); err != nil {
		return nil, err
	}

	c, err := h.markDeleting(ctx, clusterID)
	if err != nil {
		return nil, err
	}

	return h.show(ctx, c)
}

// Kubeconfig cuts a client certificate for the caller, good for seven days,
// and answers the file. Nothing is kept: a member removed from the Team keeps
// what they downloaded until it expires (docs/adr/0013).
func (h *Clusters) Kubeconfig(ctx context.Context, req *cluster.KubeconfigRequest) (*dto.Kubeconfig, error) {
	c, err := h.auth.cluster(ctx, uuid.MustParse(req.ProjectID), uuid.MustParse(req.ClusterID), authz.KubernetesWrite)
	if err != nil {
		return nil, err
	}
	if err := refuseUnlessAlive(c); err != nil {
		return nil, err
	}
	if !c.ControlPlaneReady() {
		return nil, cluster.ErrorNotRunning
	}

	raw, err := h.engine.IssueKubeconfig(ctx, c.ID, engine.KubeconfigSpec{
		User: contextutil.CurrentUserID(ctx).String(), Groups: []string{engine.AdminsGroup}, TTL: KubeconfigTTL,
		Server: "https://" + c.Address(),
	})
	if err != nil {
		return nil, engineError(err)
	}
	h.log.InfoWithFields("kubeconfig issued", logger.NewFields().Add("cluster_id", c.ID).Add("user_id", contextutil.CurrentUserID(ctx)))

	return &dto.Kubeconfig{Kubeconfig: string(raw), ExpiresAt: h.now().Add(KubeconfigTTL)}, nil
}
