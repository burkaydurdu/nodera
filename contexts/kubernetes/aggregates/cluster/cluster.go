// Package cluster is the Cluster: a Kubernetes cluster whose Control plane
// Nodera runs and whose Nodes are the Team's own Droplets, with its Node pools
// and the Nodes in them (docs/adr/0013).
package cluster

import (
	"crypto/rand"
	"encoding/hex"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/utils/str"
)

type Status string

const (
	// StatusProvisioning: the Control plane is being set up and the first
	// Nodes asked for.
	StatusProvisioning Status = "provisioning"
	// StatusRunning: the API server answers and at least one Node is ready.
	StatusRunning Status = "running"
	// StatusDegraded: the API server answers and no Node is ready.
	StatusDegraded Status = "degraded"
	StatusDeleting Status = "deleting"
	StatusDeleted  Status = "deleted"
	// StatusFailed: the Control plane could not be set up.
	StatusFailed Status = "failed"
)

// FailureCode is why a Cluster or a Node failed, for the web app and the
// emails to word in the reader's language; the reason keeps the English
// sentence for the history.
type FailureCode string

const (
	FailureControlPlane FailureCode = "control_plane_failed"
	FailureNodeRejected FailureCode = "node_rejected"
	FailureTimedOut     FailureCode = "timed_out"
)

// transitions is the one table of allowed status moves. A Cluster may be
// deleted from every status but deleting and deleted; running and degraded
// trade places as Nodes come and go.
var transitions = map[Status][]Status{
	StatusProvisioning: {StatusRunning, StatusDegraded, StatusFailed, StatusDeleting},
	StatusRunning:      {StatusDegraded, StatusDeleting},
	StatusDegraded:     {StatusRunning, StatusDeleting},
	StatusFailed:       {StatusDeleting},
	StatusDeleting:     {StatusDeleted},
	StatusDeleted:      {},
}

func (s Status) CanTransitionTo(to Status) bool {
	for _, allowed := range transitions[s] {
		if allowed == to {
			return true
		}
	}

	return false
}

// AllowsChange is whether Node pools may be added, resized or removed: only
// while the Cluster is alive.
func (s Status) AllowsChange() bool {
	return s == StatusProvisioning || s == StatusRunning || s == StatusDegraded
}

func (s Status) AllowsDelete() bool {
	return s != StatusDeleting && s != StatusDeleted
}

// Live is whether the Cluster has a Control plane to speak of.
func (s Status) Live() bool {
	return s != StatusDeleted
}

type Cluster struct {
	ID        uuid.UUID
	ProjectID uuid.UUID
	TeamID    uuid.UUID
	Region    string
	Name      string
	Version   string
	Status    Status
	// Endpoint is the host name and EndpointPort the port of the Region's
	// address that is this Cluster's alone; the port after it carries the
	// Nodes' tunnel (docs/adr/0015).
	Endpoint     string
	EndpointPort int
	// TeamSuspended is the Team's Billing status as it concerns this Cluster:
	// the droplet context powers the Nodes off, this context refuses changes.
	TeamSuspended bool
	FailureCode   *FailureCode
	FailureReason *string

	// What the engine was last made to hold; they trail the status until the
	// engine agrees, and the jobs work on the rows where they lag.
	ControlPlaneCreatedAt *time.Time
	ControlPlaneReadyAt   *time.Time
	PreparedAt            *time.Time
	ControlPlaneDeletedAt *time.Time

	CreatedBy uuid.UUID
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}

// Address is the Endpoint as a client dials it: host and port.
func (c *Cluster) Address() string {
	return net.JoinHostPort(c.Endpoint, strconv.Itoa(c.EndpointPort))
}

// TunnelPort is where the Nodes' tunnel for the API server's calls to the
// kubelets lands: the port after the Endpoint's.
func (c *Cluster) TunnelPort() int { return c.EndpointPort + 1 }

// ControlPlaneReady is whether the API server answers and the Cluster is
// prepared for Nodes to join.
func (c *Cluster) ControlPlaneReady() bool {
	return c.ControlPlaneReadyAt != nil && c.PreparedAt != nil
}

// Transition moves the Cluster and stamps what the status implies.
func (c *Cluster) Transition(to Status, now time.Time) error {
	if !c.Status.CanTransitionTo(to) {
		return ErrorInvalidTransition.Copy().SetMessage("cannot move a " + string(c.Status) + " cluster to " + string(to))
	}
	c.Status, c.UpdatedAt = to, now
	switch to {
	case StatusDeleting:
		at := now
		c.DeletedAt = &at
	case StatusDeleted:
		if c.DeletedAt == nil {
			at := now
			c.DeletedAt = &at
		}
	case StatusProvisioning, StatusRunning, StatusDegraded, StatusFailed:
	}

	return nil
}

// Fail moves the Cluster to failed and records why.
func (c *Cluster) Fail(code FailureCode, reason string, now time.Time) error {
	if err := c.Transition(StatusFailed, now); err != nil {
		return err
	}
	c.FailureCode, c.FailureReason = &code, &reason

	return nil
}

// Taint is what a Node pool puts on its Nodes to keep workloads off them
// unless they tolerate it.
type Taint struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Effect string `json:"effect"`
}

type NodePool struct {
	ID        uuid.UUID
	ClusterID uuid.UUID
	Name      string
	PlanCode  string
	Count     int
	Labels    map[string]string
	Taints    []Taint
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}

type NodeStatus string

const (
	// NodeRequested: asked of the droplet context, or about to be once the
	// Control plane is ready.
	NodeRequested NodeStatus = "requested"
	// NodeJoining: the Droplet is active, the kubelet not yet ready.
	NodeJoining  NodeStatus = "joining"
	NodeReady    NodeStatus = "ready"
	NodeNotReady NodeStatus = "not_ready"
	// NodeReleasing: being emptied and its Droplet given back.
	NodeReleasing NodeStatus = "releasing"
	// NodeGone: its Droplet is deleted.
	NodeGone   NodeStatus = "gone"
	NodeFailed NodeStatus = "failed"
)

// Counts is whether a Node in this status fills a place of its pool's count.
func (s NodeStatus) Counts() bool {
	return s == NodeRequested || s == NodeJoining || s == NodeReady || s == NodeNotReady
}

// Live is whether the Node is still shown and worked on.
func (s NodeStatus) Live() bool {
	return s != NodeGone
}

type Node struct {
	ID        uuid.UUID
	ClusterID uuid.UUID
	PoolID    uuid.UUID
	Hostname  string
	Status    NodeStatus

	// What the droplet context said about the Node's Droplet.
	DropletID     *uuid.UUID
	DropletStatus string
	IPv4          *string

	FailureCode   *FailureCode
	FailureReason *string

	// RequestedAt is when the request went to the droplet context; nil while
	// the Control plane is not yet ready to be joined.
	RequestedAt     *time.Time
	DropletActiveAt *time.Time
	ReadyAt         *time.Time
	ReleaseAskedAt  *time.Time
	ReleasedAt      *time.Time
	GoneAt          *time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

// ProviderID is how the kubelet names the Node to the API server, so a Node
// of the cluster is matched to this row and never by hostname alone.
func (n *Node) ProviderID() string {
	return ProviderIDPrefix + n.ID.String()
}

const ProviderIDPrefix = "nodera://"

// DropletRejected is the Droplet status this context records when the droplet
// context answered that no Droplet could be opened for the Node.
const DropletRejected = "rejected"

// HasDroplet is whether a Droplet exists or may still exist for the Node: one
// was reported, or one was asked for and the droplet context has not said no.
// A Node without one has nothing to give back and is gone at once.
func (n *Node) HasDroplet() bool {
	return n.DropletID != nil || (n.RequestedAt != nil && n.DropletStatus != DropletRejected)
}

// NewID is a time-ordered id, so the newest Node or pool of a Cluster is the
// one with the greatest id even when several were made in one instant.
func NewID() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.New()
	}

	return id
}

// Hostname is <pool>-<6 hex>: an RFC 1123 label, unique enough in a Cluster.
func NewHostname(pool string) string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)

	return pool + "-" + hex.EncodeToString(b)
}

// Names of Clusters and Node pools are RFC 1123 labels; a pool's is short so
// that its Nodes' hostnames stay labels too.
const (
	MaxNameLength     = 63
	MaxPoolNameLength = 32
	MinNodes          = 1
)

var label = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

func ValidName(name string, maxLength int) bool {
	return len(name) > 0 && len(name) <= maxLength && label.MatchString(name)
}

var taintEffects = map[string]bool{"NoSchedule": true, "PreferNoSchedule": true, "NoExecute": true}

// Kubernetes' own rules for a label key's name and a label or taint value;
// an optional DNS prefix before a slash is allowed on a key.
var (
	labelName   = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9_.-]{0,61}[A-Za-z0-9])?$`)
	labelPrefix = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$`)
)

const (
	MaxLabels = 10
	MaxTaints = 10
)

func validLabelKey(key string) bool {
	for i := 0; i < len(key); i++ {
		if key[i] == '/' {
			return labelPrefix.MatchString(key[:i]) && len(key[:i]) <= 253 && labelName.MatchString(key[i+1:])
		}
	}

	return labelName.MatchString(key)
}

func validLabelValue(value string) bool {
	return str.IsEmpty(value) || labelName.MatchString(value)
}

// reservedLabelPrefix reports whether a label key's prefix is one a Node may
// not set on itself: Kubernetes' own, which the kubelet refuses to start with
// (NodeRestriction), and Nodera's, which names the pool.
func reservedLabelPrefix(key string) bool {
	i := strings.LastIndex(key, "/")
	if i < 0 {
		return false
	}
	prefix := key[:i]
	for _, reserved := range []string{"kubernetes.io", "k8s.io", "nodera.cloud"} {
		if prefix == reserved || strings.HasSuffix(prefix, "."+reserved) {
			return true
		}
	}

	return false
}

// ValidateLabels refuses a label Kubernetes would refuse, before it reaches a
// Node, with the first offending key in the message.
func ValidateLabels(labels map[string]string) error {
	if len(labels) > MaxLabels {
		return ErrorInvalidLabel.Copy().SetMessage("a node pool carries at most 10 labels")
	}
	for k, v := range labels {
		if !validLabelKey(k) || !validLabelValue(v) {
			return ErrorInvalidLabel.Copy().SetMessage("label " + k + " is not a valid kubernetes label")
		}
		if reservedLabelPrefix(k) {
			return ErrorInvalidLabel.Copy().SetMessage("label " + k + " uses a prefix a node may not set on itself")
		}
	}

	return nil
}

func ValidateTaints(taints []Taint) error {
	if len(taints) > MaxTaints {
		return ErrorInvalidTaint.Copy().SetMessage("a node pool carries at most 10 taints")
	}
	for _, t := range taints {
		if !validLabelKey(t.Key) || !validLabelValue(t.Value) || !taintEffects[t.Effect] {
			return ErrorInvalidTaint.Copy().SetMessage("taint " + t.Key + " is not a valid kubernetes taint")
		}
	}

	return nil
}
