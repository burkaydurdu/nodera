---
status: accepted; the Endpoint's SNI exposure superseded by ADR-0015
---

# Kubernetes control planes run on Kamaji on Nodera's own servers; the Nodes are the customer's Droplets

Nodera's fourth product is managed Kubernetes in the shape DigitalOcean sells it: a Cluster, Node
pools, a Kubeconfig. Two decisions shape it. First, a Node is a Droplet: it appears in the Project's
Droplet list, is rented from the Provider through the droplet context and is charged as Droplet
hours at its Locked price, so the Provider adapters (ADR-0003) and the usage flow to billing exist
once. Second, the Control plane is Nodera's: it runs on servers Nodera rents and operates itself, one
fleet per Region, is never a Droplet and is not charged, as DigitalOcean charges nothing for its
control plane. That puts Kubernetes in the family of Mail (ADR-0009) and Storage (ADR-0012), where
Nodera runs the engine itself, while its Nodes stay in the family of Droplets, where Nodera resells
a Provider's servers.

The engine is Kamaji (Apache-2.0, Clastix Labs), researched on 2026-10-09. It runs each Cluster's
Control plane as pods of a management cluster from upstream, unmodified Kubernetes components set up
with kubeadm, so a Node joins with a plain `kubeadm join`, a bootstrap token and a CA hash that fit
a cloud-init script. Konnectivity is on by default, so a Node on a public address only has to reach
the Endpoint on two ports and nothing reaches into the Node. The Endpoint pattern we wanted, one
wildcard DNS record per Region and SNI routing to each Control plane through one address, is
documented through the Gateway API (TLSRoute passthrough); a pod inside a Cluster sends no host
name, so ADR-0015 keeps the one address and the wildcard record but routes by a port per Cluster.
The official benchmark runs a hundred Control planes on one etcd. Its adopters include OVHcloud, Rackspace Spot, Aruba, Namecheap, Mistral
AI and NVIDIA. We rejected vCluster, whose joining of external bare-metal machines is a licensed
feature compiled out of the open-source image; k3k, whose hosted mode is experimental and K3s rather
than upstream Kubernetes; HyperShift, which needs an OpenShift management cluster and RHCOS workers;
Gardener, which provisions nodes itself and has no path for machines opened elsewhere; and plain
kubeadm per customer, which is the same control plane with the operator, the certificate rotation,
the exposure and the multi-tenancy left for us to write.

The Nodes run Nodera's Ubuntu Image, install containerd, kubelet and kubeadm from cloud-init at first
boot and join over their public addresses; Cilium with WireGuard encryption is the one CNI, never
chosen by the customer. The Kubeconfig is a client certificate signed by the Cluster's CA, good for
seven days and bound to an RBAC group of Nodera's rather than `system:masters`, so it can be
reasoned about and never bypasses RBAC.

## Consequences

- Kamaji has had no free stable channel since July 2024: a weekly edge tag, which its release notes
  call "for testing and evaluation" and its versioning page "generally considered production
  ready". We pin one edge tag and move to another only through the contract test; no Clastix
  subscription. Kamaji is not a CNCF project and has one vendor behind it, which the ADR records as
  an accepted risk.
- A Region's Control planes share etcd in pools of at most a hundred Clusters, the benchmarked
  size. Isolation inside a pool is etcd RBAC on a key prefix, not physical, and kamaji-etcd itself
  calls multi-tenant etcd "not common practice"; a Cluster's own etcd is the costly option kept for
  a paid HA Control plane later.
- A Control plane upgrades one minor version at a time and never down. Nodera offers the two
  newest minor versions; upgrading a Cluster, and with it the Nodes, which Kamaji leaves entirely
  to the operator, is the first work after the first release.
- The Node's whole life is Nodera's: the cloud-init that installs and joins, the readiness watch,
  the drain and the Droplet's deletion. Installing from package mirrors at boot takes minutes, so a
  Node has fifteen minutes to become ready; a pre-built image or Nodera's own mirror is a later
  improvement.
- The first release sells Kubernetes in one Region, where the Control plane fleet is; a second
  Region arrives when its fleet does, and the catalog says which Regions sell Kubernetes. The
  management cluster, its address and its etcd pools are operator work documented under
  `deploy/kubernetes`, not code of the service.
- There is no block storage behind the Nodes and no load balancer: the first release sells a
  Kubernetes without persistent volumes and without a cloud load balancer, which the product says
  plainly. Cherry rents block storage in some locations, so a CSI can come later.
- The Endpoint's domain is a registered domain of Nodera's own, never under the web app's, as
  Storage's address is; a wildcard record per Region is written once by an operator and the service
  never writes DNS.
- A Kubeconfig cannot be revoked, only outlived: a member removed from a Team keeps cluster-admin
  for up to seven days, which the product says plainly. Access with an API key through the
  integration gateway is a later kind of Kubeconfig identity.
- The engine's name never reaches a DTO or an error message; the service drives it behind
  `contexts/kubernetes/engine`, with a fake for the integration suite, and a real Kamaji runs only in
  the contract test under `NODERA_KUBERNETES_SMOKE`, where the exposure and the konnectivity path
  are proven before any release moves the pinned tag.
