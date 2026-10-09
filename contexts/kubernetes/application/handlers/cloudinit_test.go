package handlers_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/piggystats/nodera/contexts/kubernetes/aggregates/cluster"
	"github.com/piggystats/nodera/contexts/kubernetes/application/handlers"
	"github.com/piggystats/nodera/contexts/kubernetes/engine"
)

func TestCloudInit_InstallsTheVersionAndJoinsWithTheNodesIdentity(t *testing.T) {
	n := &cluster.Node{ID: uuid.MustParse("019a0000-0000-7000-8000-000000000001"), Hostname: "workers-ab12cd"}
	pool := &cluster.NodePool{Name: "workers", Labels: map[string]string{"tier": "web", "env": "prod"}, Taints: []cluster.Taint{{Key: "web", Value: "true", Effect: "NoSchedule"}}}
	join := &engine.Join{Endpoint: "c1.vno1.k8s.nodera.test:6443", Token: "abcdef.0123456789abcdef", CACertHash: "sha256:00ff"}

	out := handlers.CloudInit(n, pool, "1.37.0", join)

	assert.True(t, strings.HasPrefix(out, "#cloud-config\n"))
	assert.Contains(t, out, "hostname: workers-ab12cd\n")
	assert.Contains(t, out, "https://pkgs.k8s.io/core:/stable:/v1.37/deb/")
	assert.Contains(t, out, "DEBIAN_FRONTEND=noninteractive apt-get install -y -o Dpkg::Options::=--force-confold kubelet=1.37.0-* kubeadm=1.37.0-*")
	assert.Contains(t, out, "printf 'KUBELET_EXTRA_ARGS=%s\\n' '--provider-id=nodera://019a0000-0000-7000-8000-000000000001 --node-labels=env=prod,nodera.cloud/pool=workers,tier=web --register-with-taints=web=true:NoSchedule' > /etc/default/kubelet")
	assert.Contains(t, out, "kubeadm join c1.vno1.k8s.nodera.test:6443 --token abcdef.0123456789abcdef --discovery-token-ca-cert-hash sha256:00ff --node-name workers-ab12cd")
}

// The kubelet package owns /etc/default/kubelet: it is written after the
// install, never before, or dpkg stops at a question nobody answers.
func TestCloudInit_WritesTheKubeletArgumentsAfterThePackage(t *testing.T) {
	n := &cluster.Node{ID: uuid.New(), Hostname: "w-1"}
	out := handlers.CloudInit(n, &cluster.NodePool{Name: "w"}, "1.37.0", &engine.Join{Endpoint: "e:6443", Token: "t.t", CACertHash: "sha256:a"})

	install := strings.Index(out, "apt-get install")
	args := strings.Index(out, "/etc/default/kubelet")
	join := strings.Index(out, "kubeadm join")
	assert.Positive(t, install)
	assert.Greater(t, args, install, "after the package is installed")
	assert.Greater(t, join, args, "before the kubelet starts under kubeadm")
	assert.NotContains(t, out, "path: /etc/default/kubelet", "never among the files written before the packages")
}

func TestCloudInit_WithoutTaintsRegistersNone(t *testing.T) {
	n := &cluster.Node{ID: uuid.New(), Hostname: "w-1"}
	pool := &cluster.NodePool{Name: "w"}

	out := handlers.CloudInit(n, pool, "1.36.4", &engine.Join{Endpoint: "e:6443", Token: "t.t", CACertHash: "sha256:a"})

	assert.NotContains(t, out, "--register-with-taints")
	assert.Contains(t, out, "--node-labels=nodera.cloud/pool=w")
	assert.Contains(t, out, "stable:/v1.36/")
}

func TestSettings_EndpointCarriesTheClusterIdAndTheRegion(t *testing.T) {
	s := handlers.Settings{EndpointDomain: "k8s.nodera.cloud"}
	id := uuid.MustParse("019a0000-0000-7000-8000-000000000001")

	assert.Equal(t, "019a0000-0000-7000-8000-000000000001.vno1.k8s.nodera.cloud", s.Endpoint(id, "vno1"))
}

// cloud-init reads the script as YAML: every command must survive as one
// string, the kubelet's arguments with their colons and equals signs included.
func TestCloudInit_IsYAMLWhoseCommandsReachTheShellIntact(t *testing.T) {
	n := &cluster.Node{ID: uuid.MustParse("019a0000-0000-7000-8000-000000000001"), Hostname: "gpu-ab12cd"}
	pool := &cluster.NodePool{Name: "gpu", Labels: map[string]string{"tier": "gpu"}, Taints: []cluster.Taint{{Key: "gpu", Value: "true", Effect: "NoSchedule"}}}
	out := handlers.CloudInit(n, pool, "1.37.0", &engine.Join{Endpoint: "c1.vno1.k8s.nodera.test:6443", Token: "abcdef.0123456789abcdef", CACertHash: "sha256:00ff"})

	var doc struct {
		Hostname string   `yaml:"hostname"`
		RunCmd   []string `yaml:"runcmd"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(out), &doc))
	assert.Equal(t, "gpu-ab12cd", doc.Hostname)
	assert.Contains(t, doc.RunCmd, `printf 'KUBELET_EXTRA_ARGS=%s\n' '--provider-id=nodera://019a0000-0000-7000-8000-000000000001 --node-labels=nodera.cloud/pool=gpu,tier=gpu --register-with-taints=gpu=true:NoSchedule' > /etc/default/kubelet`)
	assert.Contains(t, doc.RunCmd, "kubeadm join c1.vno1.k8s.nodera.test:6443 --token abcdef.0123456789abcdef --discovery-token-ca-cert-hash sha256:00ff --node-name gpu-ab12cd")
}

// The kubelet refuses to start with swap on, and a Provider's image may ship
// a swap file: it is turned off before the join and kept off across reboots.
func TestCloudInit_TurnsSwapOffForGoodBeforeJoining(t *testing.T) {
	n := &cluster.Node{ID: uuid.New(), Hostname: "w-1"}
	out := handlers.CloudInit(n, &cluster.NodePool{Name: "w"}, "1.37.0", &engine.Join{Endpoint: "e:6443", Token: "t.t", CACertHash: "sha256:a"})

	var doc struct {
		RunCmd []string `yaml:"runcmd"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(out), &doc))
	swap, join := -1, -1
	for i, cmd := range doc.RunCmd {
		switch {
		case cmd == `swapoff -a && sed -i '/^[^#].*\sswap\s/ s/^/#/' /etc/fstab`:
			swap = i
		case strings.HasPrefix(cmd, "kubeadm join"):
			join = i
		}
	}
	require.NotEqual(t, -1, swap, "swap is turned off and left out of fstab")
	assert.Less(t, swap, join, "before the kubelet starts under kubeadm")
}
