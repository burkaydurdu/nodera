---
status: accepted
---

# A Cluster's API server answers on its own port of the Region's address

ADR-0013 planned one address per Region with SNI routing to each Control plane through a Gateway
API listener in TLS passthrough. On the first real fleet (2026-10-09) a Node joined and `kubectl`
worked that way, but no pod inside the Cluster reached its API server. The in-cluster `kubernetes`
Service can only point at an IP and a port (Kamaji's advertise address must be an IP), and a pod
dials that Service's cluster IP and sends no host name, so a listener shared by SNI has nothing to
route it by. CoreDNS never became ready, and the tunnel agent, which needs cluster DNS, never stayed
connected.

So each Cluster gets a port of the Region's one address that is its alone, the Endpoint port, and
the port after it for its tunnel. Kamaji exposes the Control plane through a LoadBalancer Service on
the fleet's address at those ports, and the API server advertises that address and port, which
every Node and every pod reaches without a host name. The Endpoint's host name stays: one wildcard
record per Region still resolves every Cluster, the certificate names it and a Kubeconfig dials
`https://<id>.<region>.<domain>:<port>`. The kubernetes service hands the ports out from a
configured range, two at a time and unique among the Region's live Clusters; a deleted Cluster's
ports return to the range. No Gateway runs on the fleet.

## Considered options

- An address per Cluster, DigitalOcean's shape: port 443 on its own IP. Rejected for now because an
  IPv4 per Cluster is a monthly cost against a Control plane Nodera gives away, and it needs a DNS
  record per Cluster, which the service does not write. It can come later behind the same
  Endpoint.
- A proxy on every Node that re-encrypts a pod's connection and adds the host name, or tags it
  with the PROXY protocol the way Gardener's apiserver-proxy does. It puts Nodera's software on
  every customer Node, and routing by a PROXY header is not something a Gateway API controller
  offers.
- Pointing the `kubernetes` Service at the Gateway's address. A pod still sends no host name.

## Consequences

- An Endpoint carries a port instead of 443 or 6443, so a network that lets out only well-known
  ports cannot reach a Cluster.
- A Region holds as many Clusters as the range has pairs; the default 20000–29999 gives five
  thousand, far above what its etcd pools hold.
- The one-server fleet gives every Control plane's Service its own address through Cilium's node
  IPAM. A fleet of three servers puts the Region's one address in an LB IPAM pool, and every
  Service carries the same sharing key. No NodePorts are allocated, so the management cluster's
  NodePort range does not cap the Region.
