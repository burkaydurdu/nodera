package handlers

import (
	"fmt"
	"sort"
	"strings"

	"github.com/piggystats/nodera/contexts/kubernetes/aggregates/cluster"
	"github.com/piggystats/nodera/contexts/kubernetes/engine"
)

// CloudInit is the first-boot script a Node Droplet runs (docs/adr/0013):
// install containerd, kubelet and kubeadm at the Cluster's version from the
// Kubernetes package repository, then join with the one-time secret. The
// kubelet names itself by hostname and by Nodera's provider id, so the Node is
// matched to its row by id and never by name alone.
//
// /etc/default/kubelet belongs to the kubelet package: written before the
// install it stops dpkg at a question nobody answers and leaves the kubelet
// unconfigured, so it is written after. The kubelet refuses to run with swap
// on and a Provider's image may ship a swap file, so swap goes off first and
// stays out of fstab for the next boot.
func CloudInit(n *cluster.Node, pool *cluster.NodePool, version string, join *engine.Join) string {
	minor := version
	if i := strings.LastIndex(version, "."); i > 0 {
		minor = version[:i]
	}

	labels := make([]string, 0, len(pool.Labels)+1)
	labels = append(labels, "nodera.cloud/pool="+pool.Name)
	for k, v := range pool.Labels {
		labels = append(labels, k+"="+v)
	}
	sort.Strings(labels)

	taints := make([]string, 0, len(pool.Taints))
	for _, t := range pool.Taints {
		taints = append(taints, t.Key+"="+t.Value+":"+t.Effect)
	}
	sort.Strings(taints)

	extra := fmt.Sprintf("--provider-id=%s --node-labels=%s", n.ProviderID(), strings.Join(labels, ","))
	if len(taints) > 0 {
		extra += " --register-with-taints=" + strings.Join(taints, ",")
	}

	var b strings.Builder
	b.WriteString("#cloud-config\n")
	fmt.Fprintf(&b, "hostname: %s\n", n.Hostname)
	b.WriteString("package_update: true\n")
	b.WriteString("packages: [apt-transport-https, ca-certificates, curl, gpg, containerd]\n")
	b.WriteString("write_files:\n")
	b.WriteString("  - path: /etc/modules-load.d/k8s.conf\n    content: |\n      overlay\n      br_netfilter\n")
	b.WriteString("  - path: /etc/sysctl.d/k8s.conf\n    content: |\n      net.bridge.bridge-nf-call-iptables = 1\n      net.bridge.bridge-nf-call-ip6tables = 1\n      net.ipv4.ip_forward = 1\n")
	b.WriteString("runcmd:\n")
	b.WriteString("  - swapoff -a && sed -i '/^[^#].*\\sswap\\s/ s/^/#/' /etc/fstab\n")
	b.WriteString("  - modprobe overlay && modprobe br_netfilter && sysctl --system\n")
	b.WriteString("  - mkdir -p /etc/containerd && containerd config default > /etc/containerd/config.toml\n")
	b.WriteString("  - sed -i 's/SystemdCgroup = false/SystemdCgroup = true/' /etc/containerd/config.toml && systemctl restart containerd\n")
	fmt.Fprintf(&b, "  - curl -fsSL https://pkgs.k8s.io/core:/stable:/v%s/deb/Release.key | gpg --dearmor -o /etc/apt/keyrings/kubernetes-apt-keyring.gpg\n", minor)
	fmt.Fprintf(&b, "  - echo 'deb [signed-by=/etc/apt/keyrings/kubernetes-apt-keyring.gpg] https://pkgs.k8s.io/core:/stable:/v%s/deb/ /' > /etc/apt/sources.list.d/kubernetes.list\n", minor)
	fmt.Fprintf(&b, "  - apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y -o Dpkg::Options::=--force-confold kubelet=%s-* kubeadm=%s-* && apt-mark hold kubelet kubeadm\n", version, version)
	fmt.Fprintf(&b, "  - printf 'KUBELET_EXTRA_ARGS=%%s\\n' '%s' > /etc/default/kubelet\n", extra)
	b.WriteString("  - systemctl enable kubelet\n")
	fmt.Fprintf(&b, "  - kubeadm join %s --token %s --discovery-token-ca-cert-hash %s --node-name %s\n", join.Endpoint, join.Token, join.CACertHash, n.Hostname)

	return b.String()
}
