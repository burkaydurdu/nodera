---
status: accepted
---

# The droplet context opens a Node Droplet at the kubernetes context's request, over events

A Node is a Droplet (ADR-0013), so the kubernetes context needs Droplets opened and deleted, yet a
context never imports another and, until now, what crossed between contexts were facts about what
had happened, never requests. We weighed three other shapes. The kubernetes service could call the
droplet service's HTTP API with a service token, which would make the first synchronous dependency
between two services and a hidden one at start-up (what ADR-0004 was written to avoid). The
kubernetes context could rent servers from the Provider itself, which would copy the Provider
adapters into a second context and give billing a second kind of usage. The Node logic could live
in the droplet context, which would teach it Clusters, join secrets and kubelets. We chose request
events: the kubernetes context publishes, through its outbox, that a Node is requested (its id, the
Project, the Plan, the Region) or released, the droplet context consumes it and opens or deletes the
Droplet, and the kubernetes context learns the outcome from the Droplet status events the droplet
context already publishes, which now carry the Node's id.

The join secret does not ride the event. A bootstrap token in an outbox row and a RabbitMQ message
would be kept longer than it lives; instead the kubernetes context writes the Node's cloud-init to
Redis under `node_user_data:<node id>` with a one-hour TTL and the droplet context reads it when it
opens the Droplet, the way the customer context hands API keys to the gateway and the mail service
(ADR-0010). A key that has expired before the Droplet is opened fails the Node.

The kubernetes context validates a request against the catalog before it is accepted, so a customer
sees a 400 for a Plan too small for a Node rather than a failed Node a minute later. For that the
droplet context publishes its Plan and Region changes as events and the kubernetes context keeps a
catalog read model from them, with a replay command for the rows that predate the events, as
ADR-0004 did for Projects.

## Consequences

- The Droplet row carries a Node id, unique, so a redelivered request opens no second Droplet, and
  the Droplet DTO carries which Cluster and Node pool the Droplet belongs to. The web app shows a
  Node Droplet in the Droplet list and the droplet API refuses to power or delete it (409): a Node
  is released through its Cluster or not at all.
- A Node Droplet is opened with Nodera's operational SSH key only, never the customer's, so the
  droplet context's rule that a Droplet has at least one customer SSH key does not apply to it.
- Opening a Droplet can fail in the droplet context. Once a Droplet row exists (the Provider
  rejected the order), the kubernetes context learns it as a `failed` Droplet status; when no row
  can exist at all (the Team has no Payment method, the catalog does not sell the Plan, the join
  secret expired), the droplet context answers a `NodeRejected` event with the customer-safe
  reason instead, and acknowledges the request. Either way the Node fails as `node_rejected`.
- The first request-shaped events exist. They stay the exception: a request event names one thing
  by id and is idempotent on it, and the answer is always the facts the receiving context would have
  published anyway.
- On day 21 of Dunning the kubernetes context consumes billing's own request to delete the Team's
  Droplets and deletes the Team's Clusters, releasing their Nodes; the droplet context deleting the
  same Node Droplets at the same time is harmless, since deletion is idempotent on both sides.
- The catalog read model is eventually consistent, like Project access: a Region that just started
  selling Kubernetes may be refused for a moment.
