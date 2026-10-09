// Package engine is what the kubernetes context and the control plane fleet
// that runs its Clusters exchange (docs/adr/0013). The kubernetes service is
// the source of truth and the engine follows it, so the types speak Nodera's
// language: Kamaji is one adapter behind them and the fake another.
package engine

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrUnavailable is an engine that could not be asked, as opposed to one that
// answered no. Adapters wrap it so the handlers can tell the two apart.
var ErrUnavailable = errors.New("kubernetes engine unavailable")

// ErrNotFound is a Control plane, or a Node of a Cluster, the engine does not
// hold.
var ErrNotFound = errors.New("not found in the engine")

// ErrDraining is a Node whose workloads are still leaving; the caller asks
// again later or forces.
var ErrDraining = errors.New("the node is still being drained")

// ControlPlaneSpec is one Control plane to run: the Cluster it serves, the
// Kubernetes version, the Endpoint host name the API server's certificate
// names, the Endpoint port it answers on at the Region's address and the port
// after it, where the Nodes' tunnel for the API server's calls to the kubelets
// lands (docs/adr/0015).
type ControlPlaneSpec struct {
	ClusterID  uuid.UUID
	Region     string
	Version    string
	Hostname   string
	Port       int
	TunnelPort int
}

type ControlPlaneState string

const (
	// ControlPlanePending: asked for, not yet answering.
	ControlPlanePending ControlPlaneState = "pending"
	ControlPlaneReady   ControlPlaneState = "ready"
	// ControlPlaneMissing: the engine holds no Control plane for the Cluster.
	ControlPlaneMissing ControlPlaneState = "missing"
)

type ControlPlaneStatus struct {
	State   ControlPlaneState
	Version string
}

// Join is what a Node needs to join at first boot: where the API server is,
// a bootstrap token good for one use within its life, and the hash of the
// Cluster's CA so the Node trusts the right one.
type Join struct {
	Endpoint   string
	Token      string
	CACertHash string
	ExpiresAt  time.Time
}

// Node is one member of a Cluster as its API server lists it.
type Node struct {
	Name       string
	ProviderID string
	Ready      bool
}

// KubeconfigSpec is the identity a Kubeconfig is cut for: the member's id as
// the user, the group its RBAC binding names, how long it is good for, and
// the Endpoint it points at (https://<host>:<port>).
type KubeconfigSpec struct {
	User   string
	Groups []string
	TTL    time.Duration
	Server string
}

// AdminsGroup is the RBAC group a Kubeconfig puts its holder in; PrepareCluster
// binds it to cluster-admin. It is never system:masters, which bypasses RBAC.
const AdminsGroup = "nodera:cluster-admins"
