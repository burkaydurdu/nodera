# Kubernetes

The context behind Nodera's fourth product: a Team's Kubernetes cluster, whose Control plane Nodera
runs on its own servers and whose Nodes are the Team's own Droplets. It knows a Project only by its
identifier, a Droplet only through the droplet context's events and the catalog only through the
droplet context's events; Teams, Users and Roles belong to the customer context.

## Language

### Clusters

**Cluster**:
What a customer opens: one Kubernetes cluster with a Control plane Nodera runs and Node pools of
the customer's own Droplets, in one Region, on one Kubernetes version, under a name the customer
gives and may change. Belongs to exactly one Project. The product and its App of the web app carry
the name Kubernetes.
_Avoid_: k8s, tenant (the engine's word), management cluster (where Control planes run; an
operator's word, never a customer's)

**Control plane**:
The part of a Cluster Nodera runs: its API server and what stands behind it, on Nodera's own
servers in the Cluster's Region. Never a Droplet, never shown as a server, never charged.
_Avoid_: master, tenant control plane (the engine's word), hosted control plane (how it is run,
not what it is)

**Endpoint**:
The address a Cluster's API server answers at: a host name under Nodera's own Kubernetes domain
with the Cluster's identifier and Region in it, and a port of the Region's address that is the
Cluster's alone; both are fixed for the life of the Cluster.
_Avoid_: API server URL, host (bare), load balancer, server address

**Kubernetes version**:
The Kubernetes release a Cluster runs, chosen from the versions Nodera offers when the Cluster is
opened and kept until an upgrade; every Node joins at the Cluster's version. Nodera offers the two
newest minor versions; a version no longer offered opens no new Cluster and keeps running the ones
it has.
_Avoid_: release, k8s version, patch (one part of it)

**Cluster status**:
Where a Cluster is in its life: `provisioning` (the Control plane is being set up; the first Nodes
are asked for once it can be joined), `running` (the API server answers and at least one Node is ready), `degraded`
(the API server answers and no Node is ready, or the Team is suspended), `deleting`, `deleted`,
`failed` (the Control plane was not ready within thirty minutes).
_Avoid_: state, phase, health

**Failure code**:
Why a Cluster or a Node failed: `control_plane_failed` (the Control plane could not be set up),
`node_rejected` (the droplet context could not open the Node's Droplet), `timed_out` (the Node
never became ready). The words for it are the reader's language; the failure reason is the English
sentence kept for the history.
_Avoid_: error code, failure type

### Nodes

**Node pool**:
A group of Nodes of one Cluster that share a Plan, a count, labels and taints, under a name unique
in the Cluster. Choosing the count adds Nodes up to it or removes the newest down to it, counting
only the Nodes still standing, so it also replaces the ones that failed; a pool's Plan never
changes, a new pool replaces it. A Cluster has at least one Node pool.
_Avoid_: worker pool, node group, size (say the count or the Plan)

**Node**:
One member of a Node pool: a Droplet of the Cluster's Project, opened by the droplet context at
this context's request, that joins the Cluster at first boot. Managed only through its Cluster; the
Droplet it is cannot be powered or deleted on its own and carries no customer SSH key.
_Avoid_: worker, server, machine, instance, VM

**Node status**:
Where a Node is in its Cluster, beside the Droplet status the droplet context reports: `requested`
(asked of the droplet context), `joining` (the Droplet is active, the kubelet not yet ready),
`ready`, `not_ready` (ready once, not now), `releasing` (being emptied and its Droplet given
back), `gone` (its Droplet is deleted), `failed`.
_Avoid_: state, condition (Kubernetes' word for one part of it), phase

**Join**:
A Node becoming a member of its Cluster at first boot, with a one-time secret prepared for that
Node alone and good for one hour. A Node that has not joined within fifteen minutes of its Droplet
being active has failed.
_Avoid_: bootstrap, enrol, register, provision (the Droplet's word)

**Release**:
Removing a Node from its Cluster: its workloads are moved off it, within five minutes and then by
force, the Cluster forgets it, and the droplet context is asked to delete its Droplet.
_Avoid_: drain (one step of it), delete (the Droplet's word), scale down (the pool's count)

### Access

**Kubeconfig**:
What a member downloads to reach a Cluster as its administrator: the Endpoint, the Cluster's
certificate authority and a client certificate made for that member at that moment, good for seven
days and never kept by Nodera. Needs `kubernetes.write`; a member removed from the Team keeps what
they downloaded until it expires.
_Avoid_: credentials, token, admin config, certificate (one part of it)

**Project access**:
What the kubernetes context knows about a Project: which Team it belongs to and which Users are
members with which Role. Learned from the customer context's events, never queried, exactly as the
droplet context does. `kubernetes.read` to list and view, `kubernetes.write` to open, change and
delete Clusters and to download a Kubeconfig.
_Avoid_: membership (the customer context's word), permission

**Catalog**:
What the kubernetes context knows of the droplet context's Plans and Regions, learned from its
events, never queried: which Regions sell Kubernetes and which Plans a Node may have (at least two
CPUs and 4 GB of RAM). The Image a Node runs is Nodera's; a customer never chooses it.
_Avoid_: plan list, inventory, offer (the droplet context's word for a Provider's listing)

### Billing

**Billing status**:
What the billing context last said about a Team: `no_payment_method` (the default for a Team never
heard about), `active` or `suspended`. Learned from events, never queried. No payment method
refuses opening a Cluster or adding Nodes; suspended refuses them too, and while the droplet
context powers the Team's Droplets off the Cluster is `degraded` and its Control plane keeps
running; a Team still suspended on the day its Droplets are deleted loses its Clusters with them.
A Node costs what its Droplet costs; a Control plane costs nothing.
_Avoid_: account status, blocked, frozen
