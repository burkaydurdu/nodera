# The Kubernetes control plane fleet

A Cluster's Control plane runs on Nodera's own management cluster, one per Region, as a Kamaji
`TenantControlPlane` (docs/adr/0013). The management cluster is operator work: the kubernetes
service only speaks to it over the Kubernetes API with the credentials in `[engine.kamaji]`.

## What a Region's fleet needs

- A Kubernetes cluster on Nodera's own servers in the Region (kubeadm, three control-plane nodes,
  Cilium), with the Kamaji operator installed at **the pinned edge tag** named in
  `KAMAJI_TAG` below. Kamaji publishes no free stable channel; a tag moves only after
  `make test-kamaji-contract` passes against it.
- One or more `DataStore` resources, each an etcd of three members, holding at most a hundred
  Control planes (the benchmarked size). The service names the pool a Cluster goes to in
  `[engine.kamaji] datastore`.
- One public address for the Region that every Control plane's `LoadBalancer` Service shares,
  each on its own ports (docs/adr/0015): the service hands every Cluster a port from
  `[engine] first_endpoint_port`..`last_endpoint_port` for its API server and the next one for its
  konnectivity tunnel, and the API server advertises that address and port to the Nodes and pods.
  On one server Cilium's node IPAM gives the Services the server's address; on three, an LB IPAM
  pool of that one address, announced by Cilium, shares it by the `lbipam.cilium.io/sharing-key`
  every Service carries. No NodePort is allocated.
- One wildcard A record, `*.<region>.<endpoint_domain>`, pointing at that address, so the Endpoint
  `<cluster id>.<region>.<endpoint_domain>:<port>` resolves for every Cluster. The service never
  writes DNS. The domain is Nodera's own Kubernetes domain, never the web app's.
- A namespace for the Region's Control planes (`[engine.kamaji] namespace`) and a service account
  the service authenticates as, allowed to manage `tenantcontrolplanes` and to read `secrets` in
  that namespace; its token and the cluster's CA go into `[engine.kamaji] token` and
  `ca_certificate` through the environment, never a tracked file.
- Outbound reach from the Nodes: a Node runs on its public address and only needs to reach the
  Region's address on its Cluster's two ports; nothing reaches into a Node, the API server's calls
  to the kubelet ride the konnectivity tunnel.

## Addons

`addons/` holds the manifests the service applies into every new tenant before a Node joins
(`PrepareCluster`): the CNI, and nothing a customer would expect to choose. Cilium is the one
CNI, rendered with kube-proxy replacement and WireGuard encryption, so the Nodes' public addresses
carry no plain pod traffic. A manifest may name the tenant's own API server as
`__NODERA_API_HOST__` and `__NODERA_API_PORT__`; the service writes in the address and port the
API server advertises, so the CNI comes up without DNS.

`addons/10-cilium.yaml` is rendered from Cilium's Helm chart; its header carries the exact
command. A new Cilium version is rendered the same way and replaces the file whole.

The service also binds the group `nodera:cluster-admins` to `cluster-admin`; every Kubeconfig it
cuts puts its holder in that group, never in `system:masters`.

## Standing a Region's fleet up

`fleet/fleet.sh` builds a Region's fleet on one Cherry server, the same components and wiring as
above: kubeadm with Cilium, no kube-proxy and node IPAM for the Control planes' Services,
cert-manager, Kamaji at the pinned tag with its bundled etcd `DataStore`, the Region's namespace and
the service account. `fleet/install.sh` is what runs on the server; it is idempotent, so a
failed step is fixed and the install run again.

```
make k8s-fleet-up [REGION=vno1]        order, install and write the config
make k8s-fleet-install [REGION=vno1]   finish or repeat the install
make k8s-fleet-config [REGION=vno1]    write [engine] and [engine.kamaji] into the kubernetes dev.toml
make k8s-fleet-status [REGION=vno1]
make k8s-fleet-destroy [REGION=vno1]   delete the server at Cherry
```

Cherry credentials come from `CHERRY_TOKEN` and `CHERRY_PROJECT_ID`, or in development from the
droplet service's dev.toml. The server is reached with the operational key
(`~/.ssh/nodera_ops_ed25519`, the one `[nodes] ops_public_key` names). Without
`FLEET_ENDPOINT_DOMAIN` the Endpoint domain is the server's address on sslip.io, which answers the
wildcard record without a DNS change; a production Region sets its own domain and the wildcard A
record instead.

One server is one control-plane node and a one-member etcd: enough to run Clusters end to end,
not the three of each a production Region needs before it takes customers.

## The pinned Kamaji tag

```
KAMAJI_TAG=26.10.2-edge
```

Install or move it with Kamaji's Helm chart at that tag only. The contract test
(`make test-kamaji-contract`) runs the adapter against a management cluster named by
`NODERA_KAMAJI_API_SERVER`, `NODERA_KAMAJI_TOKEN`, `NODERA_KAMAJI_CA_CERTIFICATE` and
`NODERA_KAMAJI_NAMESPACE`, plus the datastore, `NODERA_KAMAJI_REGION`,
`NODERA_KAMAJI_ENDPOINT_DOMAIN` and `NODERA_KAMAJI_PORT` (by default a fresh pair from
29900–29999, the top of the service's range). It applies the addons in this directory into the tenant it opens. A fleet stood up with `fleet/fleet.sh` is what
it runs against; the values are the ones `make k8s-fleet-config` writes.
