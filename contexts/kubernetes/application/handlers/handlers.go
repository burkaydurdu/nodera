// Package handlers holds the kubernetes context's business rules: each
// handler authorizes, changes the database in one transaction, announces what
// happened after the commit (docs/adr/0001) and only then asks the engine to
// follow, so that an engine that cannot be reached never leaves the database
// and the customer disagreeing. The jobs repeat what the engine did not take.
package handlers

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/kubernetes/aggregates/access"
	"github.com/piggystats/nodera/contexts/kubernetes/aggregates/billing"
	"github.com/piggystats/nodera/contexts/kubernetes/aggregates/cluster"
	"github.com/piggystats/nodera/contexts/kubernetes/engine"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/authz"
	"github.com/piggystats/nodera/pkg/contextutil"
)

// Clock is the time the handlers act at; the integration suite moves it.
type Clock func() time.Time

// Announcer publishes what this context tells others, through the outbox.
//
//go:generate mockgen -destination=../../mock/handlers/announcer.go -package=mockhandlers github.com/piggystats/nodera/contexts/kubernetes/application/handlers Announcer
type Announcer interface {
	NodeRequested(ctx context.Context, c *cluster.Cluster, pool *cluster.NodePool, n *cluster.Node, image string, at time.Time) error
	NodeReleased(ctx context.Context, nodeID uuid.UUID, at time.Time) error
	ClusterStatusChanged(ctx context.Context, project *access.ProjectAccess, c *cluster.Cluster, from *cluster.Status, at time.Time) error
	ClusterChanged(ctx context.Context, teamID, projectID, clusterID uuid.UUID, at time.Time) error
}

// Engine is the control plane fleet behind the Clusters (docs/adr/0013). Every
// call may be repeated: the jobs ask again until the engine holds what the
// database says, so asking for what already holds is not an error. A failure
// to reach the engine wraps engine.ErrUnavailable.
//
//go:generate mockgen -destination=../../mock/handlers/engine.go -package=mockhandlers github.com/piggystats/nodera/contexts/kubernetes/application/handlers Engine
type Engine interface {
	CreateControlPlane(ctx context.Context, spec engine.ControlPlaneSpec) error
	ControlPlaneStatus(ctx context.Context, clusterID uuid.UUID) (*engine.ControlPlaneStatus, error)
	// PrepareCluster installs what every Cluster runs before a Node joins: the
	// network and the RBAC binding a Kubeconfig's group relies on.
	PrepareCluster(ctx context.Context, clusterID uuid.UUID) error
	DeleteControlPlane(ctx context.Context, clusterID uuid.UUID) error
	CreateJoin(ctx context.Context, clusterID uuid.UUID, ttl time.Duration) (*engine.Join, error)
	IssueKubeconfig(ctx context.Context, clusterID uuid.UUID, spec engine.KubeconfigSpec) ([]byte, error)
	ListNodes(ctx context.Context, clusterID uuid.UUID) ([]engine.Node, error)
	// DrainNode cordons the Node and moves its workloads off; it answers
	// engine.ErrDraining while some remain, unless forced.
	DrainNode(ctx context.Context, clusterID uuid.UUID, name string, force bool) error
	DeleteNode(ctx context.Context, clusterID uuid.UUID, name string) error
}

// UserDataStore is where a Node's cloud-init waits for the droplet context
// (docs/adr/0014).
//
//go:generate mockgen -destination=../../mock/handlers/user_data_store.go -package=mockhandlers github.com/piggystats/nodera/contexts/kubernetes/application/handlers UserDataStore
type UserDataStore interface {
	Set(ctx context.Context, key string, value any, expiration time.Duration) error
}

// Settings are what the operator fixes per deployment: the one Region the
// fleet serves, the domain Endpoints are under, the range of the Region
// address's ports they answer on (both ends included), the Image every Node
// runs and the limits.
type Settings struct {
	Region             string
	EndpointDomain     string
	FirstEndpointPort  int
	LastEndpointPort   int
	NodeImage          string
	ClustersPerProject int
	PoolsPerCluster    int
	NodesPerPool       int
}

// Endpoint is the host name a Cluster's API server answers at:
// <cluster id>.<region>.<domain>, fixed for the life of the Cluster.
func (s Settings) Endpoint(clusterID uuid.UUID, region string) string {
	return clusterID.String() + "." + region + "." + s.EndpointDomain
}

// ControlPlaneSpec is the Control plane a Cluster asks the engine for.
func ControlPlaneSpec(c *cluster.Cluster) engine.ControlPlaneSpec {
	return engine.ControlPlaneSpec{
		ClusterID: c.ID, Region: c.Region, Version: c.Version,
		Hostname: c.Endpoint, Port: c.EndpointPort, TunnelPort: c.TunnelPort(),
	}
}

// The waits the jobs enforce.
const (
	// JoinTTL is how long a Node's join secret and cloud-init are good for.
	JoinTTL = time.Hour
	// ControlPlaneTimeout is how long a Control plane may stay pending.
	ControlPlaneTimeout = 30 * time.Minute
	// NodeJoinTimeout is how long a Node may take to be ready after its
	// Droplet is active; cloud-init installs from package mirrors at boot.
	NodeJoinTimeout = 15 * time.Minute
	// DrainGrace is how long a Release waits for workloads to leave before it
	// forces them.
	DrainGrace = 5 * time.Minute
	// KubeconfigTTL is how long a Kubeconfig's certificate is good for.
	KubeconfigTTL = 7 * 24 * time.Hour
)

// authorizer answers the caller's membership in a Project's Team when their
// Role holds the Permission (docs/adr/0005). A stranger and an unknown Project
// are the same 404; a member without the Permission gets a 403.
type authorizer struct {
	access   access.IService
	clusters cluster.IService
}

func (a authorizer) project(ctx context.Context, projectID uuid.UUID, permission authz.Permission) (*access.Membership, error) {
	membership, err := a.access.FindProjectMembership(ctx, projectID, contextutil.CurrentUserID(ctx))
	if err != nil {
		return nil, err
	}
	if !membership.Allows(permission) {
		return nil, access.ErrorForbidden
	}

	return membership, nil
}

// cluster is a Cluster of a Project the caller may act on with the Permission.
func (a authorizer) cluster(ctx context.Context, projectID, clusterID uuid.UUID, permission authz.Permission) (*cluster.Cluster, error) {
	if _, err := a.project(ctx, projectID, permission); err != nil {
		return nil, err
	}

	return a.clusters.Find(ctx, projectID, clusterID)
}

// requireActiveBilling is the gate on anything that would be charged.
func requireActiveBilling(ctx context.Context, billingService billing.IService, teamID uuid.UUID) error {
	status, err := billingService.StatusOf(ctx, teamID)
	if err != nil {
		return err
	}

	switch status {
	case billing.StatusActive:
		return nil
	case billing.StatusSuspended:
		return billing.ErrorTeamSuspended
	default:
		return billing.ErrorPaymentMethodRequired
	}
}

// engineError is what a customer learns of an engine that failed: that
// Kubernetes is unavailable and the request may be repeated, or that it
// refused and repeating will not help; never why.
func engineError(err error) error {
	if errors.Is(err, engine.ErrUnavailable) {
		return cluster.ErrorUnavailable.Copy().SetError(err)
	}

	return cluster.ErrorEngineRefused.Copy().SetError(err)
}

// Lifecycle is the shared core the binary builds once and hands to every
// handler and job; Orderer asks the droplet context for Nodes.
type (
	Lifecycle = lifecycle
	Orderer   = orderer
)

func NewLifecycle(log logger.ILog, accessService access.IService, clusterService cluster.IService, transactions postgresql.ITransactionManager, announcer Announcer, eng Engine, now Clock) *Lifecycle {
	return newLifecycle(log, accessService, clusterService, transactions, announcer, eng, now)
}

func NewOrderer(life *Lifecycle, userData UserDataStore, settings Settings) *Orderer {
	return newOrderer(life, userData, settings)
}
