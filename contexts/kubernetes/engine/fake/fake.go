// Package fake is the engine the integration suite runs against: it keeps
// Control planes and the Nodes a test says joined them in memory, becomes
// ready only when the test says so, and can be made to fail as unreachable
// control plane servers would.
package fake

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/kubernetes/engine"
)

// ControlPlane is what the fake holds for one Cluster, as a test reads it back.
type ControlPlane struct {
	Spec     engine.ControlPlaneSpec
	Ready    bool
	Prepared bool
	Nodes    map[string]*Member
	// Joins are the join secrets handed out, newest last.
	Joins []engine.Join
	// Kubeconfigs are the identities Kubeconfigs were cut for.
	Kubeconfigs []engine.KubeconfigSpec
}

// Member is one Node of a fake cluster: whether its kubelet reports ready and
// how many workloads are still on it, which a drain must move off first.
type Member struct {
	ProviderID string
	Ready      bool
	Pods       int
	Cordoned   bool
}

type Fake struct {
	mu      sync.Mutex
	planes  map[uuid.UUID]*ControlPlane
	failure error
	calls   []string
	now     func() time.Time
	// selfRunning makes a Control plane ready the first time it is asked
	// about, for a development environment with no fleet and no test to
	// call Advance; Nodes still need a kubelet to report in.
	selfRunning bool
	// stored is the Control plane the database holds for a Cluster, which the
	// self-running fake makes up when it was never asked to create it.
	stored func(clusterID uuid.UUID) engine.ControlPlaneSpec
}

func New() *Fake {
	return &Fake{planes: map[uuid.UUID]*ControlPlane{}, now: time.Now}
}

// NewSelfRunning is the fake a development binary runs: Control planes come
// up by themselves, so a Cluster can be opened and a Kubeconfig cut without a
// fleet. No Node ever joins, since nothing runs a kubelet.
//
// The server and the worker each run their own fake and lose it on a restart,
// while the database still says a Control plane is ready; so a Cluster the
// fake was never asked to create is one it holds, ready, as stored answers it.
func NewSelfRunning(stored func(clusterID uuid.UUID) engine.ControlPlaneSpec) *Fake {
	f := New()
	f.selfRunning = true
	f.stored = stored

	return f
}

// plane answers the Control plane of a Cluster, making one up when the
// self-running fake was never asked to create it; the caller holds the lock.
func (f *Fake) plane(clusterID uuid.UUID) (*ControlPlane, bool) {
	p, ok := f.planes[clusterID]
	if ok || !f.selfRunning {
		return p, ok
	}
	spec := f.stored(clusterID)
	spec.ClusterID = clusterID
	p = &ControlPlane{Spec: spec, Ready: true, Prepared: true, Nodes: map[string]*Member{}}
	f.planes[clusterID] = p

	return p, true
}

// ---------------------------------------------------------------------------
// Scripting
// ---------------------------------------------------------------------------

// Fail makes every call answer err until Reset; engine.ErrUnavailable is the
// one tests use for servers that cannot be reached.
func (f *Fake) Fail(err error) { f.mu.Lock(); f.failure = err; f.mu.Unlock() }

// Advance makes every pending Control plane ready, as the fleet would in time.
func (f *Fake) Advance() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.planes {
		p.Ready = true
	}
}

// Join is a kubelet reporting in: the Node named by its hostname and
// provider id appears in the Cluster, ready or not.
func (f *Fake) Join(clusterID uuid.UUID, name, providerID string, ready bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.planes[clusterID]
	if !ok {
		return
	}
	m, ok := p.Nodes[name]
	if !ok {
		m = &Member{ProviderID: providerID}
		p.Nodes[name] = m
	}
	m.Ready = ready
}

// SetPods puts workloads on a Node that a drain has to move off.
func (f *Fake) SetPods(clusterID uuid.UUID, name string, pods int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.planes[clusterID]; ok {
		if m, ok := p.Nodes[name]; ok {
			m.Pods = pods
		}
	}
}

// Plane exposes a Control plane to assertions.
func (f *Fake) Plane(clusterID uuid.UUID) (ControlPlane, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.planes[clusterID]
	if !ok {
		return ControlPlane{}, false
	}
	out := *p
	out.Nodes = map[string]*Member{}
	for k, v := range p.Nodes {
		m := *v
		out.Nodes[k] = &m
	}

	return out, true
}

// Calls answers every call made since Reset, in order.
func (f *Fake) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.calls...)
}

func (f *Fake) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.planes = map[uuid.UUID]*ControlPlane{}
	f.failure = nil
	f.calls = nil
}

// token is a bootstrap token part as kubeadm checks it: lower-case hex is
// within its [a-z0-9] alphabet.
func token(n int) string {
	return strings.ReplaceAll(uuid.NewString(), "-", "")[:n]
}

func (f *Fake) call(name string) error {
	f.calls = append(f.calls, name)

	return f.failure
}

// ---------------------------------------------------------------------------
// engine.Engine
// ---------------------------------------------------------------------------

func (f *Fake) CreateControlPlane(_ context.Context, spec engine.ControlPlaneSpec) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("CreateControlPlane " + spec.ClusterID.String()); err != nil {
		return err
	}
	if _, ok := f.planes[spec.ClusterID]; !ok {
		f.planes[spec.ClusterID] = &ControlPlane{Spec: spec, Nodes: map[string]*Member{}}
	}

	return nil
}

func (f *Fake) ControlPlaneStatus(_ context.Context, clusterID uuid.UUID) (*engine.ControlPlaneStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("ControlPlaneStatus " + clusterID.String()); err != nil {
		return nil, err
	}
	p, ok := f.plane(clusterID)
	if !ok {
		return &engine.ControlPlaneStatus{State: engine.ControlPlaneMissing}, nil
	}
	if f.selfRunning {
		p.Ready = true
	}
	if !p.Ready {
		return &engine.ControlPlaneStatus{State: engine.ControlPlanePending, Version: p.Spec.Version}, nil
	}

	return &engine.ControlPlaneStatus{State: engine.ControlPlaneReady, Version: p.Spec.Version}, nil
}

func (f *Fake) PrepareCluster(_ context.Context, clusterID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("PrepareCluster " + clusterID.String()); err != nil {
		return err
	}
	p, ok := f.plane(clusterID)
	if !ok {
		return engine.ErrNotFound
	}
	p.Prepared = true

	return nil
}

func (f *Fake) DeleteControlPlane(_ context.Context, clusterID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("DeleteControlPlane " + clusterID.String()); err != nil {
		return err
	}
	delete(f.planes, clusterID)

	return nil
}

func (f *Fake) CreateJoin(_ context.Context, clusterID uuid.UUID, ttl time.Duration) (*engine.Join, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("CreateJoin " + clusterID.String()); err != nil {
		return nil, err
	}
	p, ok := f.plane(clusterID)
	if !ok || !p.Ready {
		return nil, engine.ErrNotFound
	}
	sum := sha256.Sum256([]byte(clusterID.String()))
	join := engine.Join{
		Endpoint:   net.JoinHostPort(p.Spec.Hostname, strconv.Itoa(p.Spec.Port)),
		Token:      fmt.Sprintf("%s.%s", token(6), token(16)),
		CACertHash: "sha256:" + hex.EncodeToString(sum[:]),
		ExpiresAt:  f.now().Add(ttl),
	}
	p.Joins = append(p.Joins, join)

	return &join, nil
}

func (f *Fake) IssueKubeconfig(_ context.Context, clusterID uuid.UUID, spec engine.KubeconfigSpec) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("IssueKubeconfig " + clusterID.String()); err != nil {
		return nil, err
	}
	p, ok := f.plane(clusterID)
	if !ok || !p.Ready {
		return nil, engine.ErrNotFound
	}
	p.Kubeconfigs = append(p.Kubeconfigs, spec)

	return fmt.Appendf(nil, "apiVersion: v1\nkind: Config\nclusters:\n- name: %s\n  cluster:\n    server: %s\n    certificate-authority-data: ZmFrZQ==\nusers:\n- name: %s\n  user:\n    client-certificate-data: ZmFrZQ==\n    client-key-data: ZmFrZQ==\ncontexts:\n- name: %s\n  context:\n    cluster: %s\n    user: %s\ncurrent-context: %s\n",
		clusterID, spec.Server, spec.User, clusterID, clusterID, spec.User, clusterID), nil
}

func (f *Fake) ListNodes(_ context.Context, clusterID uuid.UUID) ([]engine.Node, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("ListNodes " + clusterID.String()); err != nil {
		return nil, err
	}
	p, ok := f.plane(clusterID)
	if !ok {
		return nil, engine.ErrNotFound
	}
	names := make([]string, 0, len(p.Nodes))
	for name := range p.Nodes {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]engine.Node, 0, len(names))
	for _, name := range names {
		m := p.Nodes[name]
		out = append(out, engine.Node{Name: name, ProviderID: m.ProviderID, Ready: m.Ready})
	}

	return out, nil
}

// DrainNode cordons the Node and moves one workload off per call, so a test
// sees a drain take more than one run; force moves them all.
func (f *Fake) DrainNode(_ context.Context, clusterID uuid.UUID, name string, force bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call(fmt.Sprintf("DrainNode %s %s force=%t", clusterID, name, force)); err != nil {
		return err
	}
	p, ok := f.planes[clusterID]
	if !ok {
		return engine.ErrNotFound
	}
	m, ok := p.Nodes[name]
	if !ok {
		return engine.ErrNotFound
	}
	m.Cordoned = true
	if force {
		m.Pods = 0
	} else if m.Pods > 0 {
		m.Pods--
	}
	if m.Pods > 0 {
		return engine.ErrDraining
	}

	return nil
}

func (f *Fake) DeleteNode(_ context.Context, clusterID uuid.UUID, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("DeleteNode " + clusterID.String() + " " + name); err != nil {
		return err
	}
	p, ok := f.planes[clusterID]
	if !ok {
		return engine.ErrNotFound
	}
	if _, ok := p.Nodes[name]; !ok {
		return engine.ErrNotFound
	}
	delete(p.Nodes, name)

	return nil
}
