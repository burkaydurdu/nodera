---
status: accepted
---

# Live updates reach the realtime service over RabbitMQ

The web app learns what changed while it is open (a new In-app Notification, a Droplet changing
status, a Team's Billing status) through one WebSocket served by `cmd/realtime`. Contexts send
messages for it the way they send everything else, through their outbox and RabbitMQ, so there is a
single runtime path between processes. Inside the realtime service, Redis pub/sub only fans a
message out to every replica, because a RabbitMQ queue hands each message to one consumer. We
weighed letting contexts publish straight to Redis pub/sub, which is faster and needs no outbox, and
kept one coupling mechanism instead. Such a message is a nudge: the database stays the source of
truth and the web app refetches over REST whenever it reconnects.

## Consequences

- Realtime is platform infrastructure, like the gateway, not a bounded context. It consumes one
  generic message (an address, a type, a payload) and knows nothing about Droplets or Invoices; the
  address is a User, or a Team together with the Permission a member needs to receive it.
- A live update arrives no sooner than the producer's outbox relays it, so the outbox relays poll
  every second rather than the library's ten.
- The notification context has no outbox: its worker publishes the In-app push straight to
  RabbitMQ after recording the Notification, and keeps a delivery state per Channel, so a
  redelivery from the error queue retries only the Channels not yet delivered and never sends an
  email twice. Its Inbox API publishes a read the same way, best effort: a tab that misses it
  catches up on its next refetch.
- A browser cannot set headers on a WebSocket handshake, so the access token arrives in the first
  frame and again after every refresh. Because the connection outlives the 15-minute token, the
  server re-checks the session (ADR-0002) and `pv` (ADR-0005) on every heartbeat and closes the
  socket when either is stale.
- The socket is one connection per tab carrying typed messages, so the support conversation widget
  later adds message types and a conversation address, not a second connection.
