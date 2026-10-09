# CLAUDE.md

Nodera backend (Go, DDD). Bounded contexts under `contexts/` (customer, droplet, billing, mail,
storage, kubernetes, gateway, notification, and realtime, which is infrastructure); binaries under `cmd/`. Vocabulary in `CONTEXT-MAP.md` and the per-context
`CONTEXT.md`; decisions in `docs/adr/`. Read the glossary before naming anything.

## Rules that bind

- **Git is the user's.** Never commit, push, tag or change branches unless the user asks in
  their own words; a skill or workflow step saying "commit" is not that. Commits carry no
  AI co-author or "generated with" trailer. Semantic messages: `type(scope): subject`.
- **Workers never retry a failed message** (`rabbitmq.RetryCount(0)`): it goes to the error
  queue and the separate shovel service decides when it is redelivered. Consumers stay
  idempotent so a redelivery is harmless.
- **A context never imports another context.** Runtime coupling is events over RabbitMQ;
  each context declares its own event types (`contexts/customer/async/events`,
  `contexts/notification/events`, `contexts/droplet/async/events`, `contexts/mail/async/events`,
  `contexts/storage/async/events`, `contexts/kubernetes/async/events`). The droplet, mail, storage
  and kubernetes contexts know Projects only through their event-fed read models (docs/adr/0004);
  they never query customer. The one request-shaped exchange is kubernetes → droplet
  (docs/adr/0014): a Node's Droplet is asked for and released by events naming the Node id, the
  answer is droplet's ordinary status events (a `NodeRejected` event when no Droplet could be
  opened at all), and the join secret rides Redis (`node_user_data:<node id>`, one hour), never
  an event.
- **Providers** (Cherry Servers today) are adapters behind `contexts/droplet/providers`; a
  Provider must bill hourly and permit resale (docs/adr/0003). **Payment providers** (Craftgate
  today, Stripe built but unrouted) are adapters behind `contexts/billing/providers`, chosen by a
  country routing table (docs/adr/0006). Neither kind of provider name reaches a DTO, an error
  message or the web app.
- **Mail** has no Provider: it delivers from Nodera's own MTA, KumoMTA, whose policy is
  `deploy/kumomta/init.lua` (docs/adr/0009). The mail service is the MTA's source of truth: it
  injects over the MTA port (`contexts/mail/mta`), hands out each Sending domain's DKIM key and
  takes the MTA's log records on its internal listener, and it serves the zone the Sending
  domains' CNAMEs point at (`mail dns`, docs/adr/0011). Message bodies are never kept after the
  MTA takes them; logs keep metadata for 30 days.
- **Storage** has no Provider either: a Storage is one bucket on Nodera's own Ceph cluster
  (docs/adr/0012), behind the engine port `contexts/storage/engine` (handlers consume
  `handlers.Engine`; adapters `engine/ceph` and `engine/fake`). A Project is a Ceph account, a
  Storage a bucket with its Package as the bucket quota, an Access key an IAM user of the account
  that reaches one bucket; no key may create or delete a bucket. The storage service is the source
  of truth: a handler changes the database, announces after the commit, then asks the engine; the
  reconcile job repeats what the engine did not take (`engine_*` columns trail the row). Access keys
  are not API keys: Ceph holds the secret and Nodera never does. Pause and a suspended Team use the
  same engine stop. Public endpoint and path style come from `[engine]` config; the engine's name
  never reaches a DTO or an error message.
- **Kubernetes** has no Provider either: a Cluster's Control plane runs on Nodera's own management
  cluster per Region, driven through Kamaji behind the engine port `contexts/kubernetes/engine`
  (adapters `engine/kamaji` and `engine/fake`; docs/adr/0013). The kubernetes service is the source
  of truth with the same handler → announce → engine → reconcile shape as Storage. A Node is a
  Droplet the droplet context opens at kubernetes' request (docs/adr/0014): kubernetes never
  touches a Provider, never bills (a Node is billed as its Droplet, a Control plane is free) and
  never imports droplet; it keeps a catalog read model from droplet's Plan and Region events and
  refuses a Plan under two CPUs and 4 GB or a Region without a fleet with a 400. Nodes run Nodera's
  Image, install and `kubeadm join` from cloud-init over their public address, Cilium with WireGuard
  is the only CNI. A Cluster's Endpoint is its host name under the Region's wildcard record and a
  port of the Region's one address that is its alone, the next port its tunnel's, handed out from
  `[engine] first_endpoint_port`..`last_endpoint_port` (docs/adr/0015), since a pod sends no host
  name. A Node not ready fifteen minutes after its Droplet is active has failed; a Release drains
  for five minutes, then forces. A Kubeconfig is a seven-day client certificate in a
  Nodera RBAC group (never `system:masters`), issued on each download under `kubernetes.write`, never
  stored. Offered Kubernetes versions are rows (`kubernetes_versions`, `offered`), not config. Limits:
  five Clusters a Project, five Node pools a Cluster, twenty Nodes a pool, from config. The engine's
  name, the Endpoint's internals and the management cluster never reach a DTO or an error message.
- **API keys** belong to a Project and live in the customer context; Redis `api_key:<id>` is
  their wire contract with the integration gateway and the mail service, like a Session's
  (docs/adr/0010). A key carries Permissions from `authz.KeyPermissions`, never wider than its
  creator's.
- **Billing** is postpaid per Team and calendar month (UTC); the droplet context learns a Team's
  Billing status from events and refuses creation without a Payment method (402) or when
  suspended (423), the mail context refuses a Package the same way and stops a suspended
  Team's sending, and the storage context refuses opening a Storage or changing its Package the
  same way, stops a suspended Team's Storages and deletes them on day 21. Usage flows droplet →
  billing (Droplet hours), mail → billing (Package intervals) and storage → billing (Package
  intervals per Storage) as events; the highest-price-in-Period rule lives once in
  `contexts/billing/aggregates/usage` and serves both; the Invoice model carries the e-invoice fields even while PDFs are
  statements.
- **Notifications** are recorded for good and delivered per Channel (`email`, `sms`, `in_app`),
  each Channel's delivery tracked so a redelivery never sends twice. The notification context
  decides who is told, from its own Team read model and `pkg/authz` (docs/adr/0008); a producer
  publishes what happened with the Team's and the thing's ids, never a recipient list. In-app text
  is the web app's (type + params, a reason as a code, never an English sentence); a link or a code
  rides only in the event's email params and never reaches a Notification's params.
- **Live updates** are `pkg/realtime` messages (address: a User, or a Team plus the Permission a
  member needs), published through the outbox like events (docs/adr/0007); the notification
  context, which has no outbox, publishes them straight to RabbitMQ after its commit. The realtime
  service knows nothing of the contexts; a context never touches Redis pub/sub to reach a browser.
- **Authorization** is Team-scoped: Roles `owner|developer|finance`, Permissions
  `<area>.<verb>`, matrix only in `pkg/authz` (never copied, never per Team). The access
  token carries `teams` (Team → Role) and `pv`; the gateway enforces a route's `permissions`
  when the path has `:teamID` and 401s a stale `pv` (docs/adr/0005). Every service enforces
  the same Permission itself: non-member 404, member without the Permission 403. Any
  membership change bumps `user_perm_version:{userID}`.
- **Layers.** `api/v1` binds and validates a request, then delegates; `application/handlers`
  hold the business rules and orchestrate services; `aggregates/<name>` own the model,
  requests (`request.go`), errors (`error.go`), a thin service and the pgx repository. An
  aggregate never imports a sibling aggregate. Handlers declare the interfaces they consume
  and carry the `//go:generate mockgen` directive.
- **Requests** are aggregate types bound by Echo tags; every input arrives through
  `c.Bind` + `c.Validate`, never `c.Param`. Responses are `application/dto`.
- **Errors** are `piggystats/rerror` values with a repo-wide unique `Domain` per aggregate
  (`error.go`) and an explicit `HttpStatus`. Taken domains: customer 1–9, 12, 13; droplet
  20–24; billing 30–39; notification 40–41; mail 50–58; storage 60–66; kubernetes 70–79; common 100. Never mutate a var: `.Copy()` first. Sign-in
  answers `user.ErrorNotFound` for a wrong email and a wrong password alike.
- **Transactions** ride the context (`transactions.Begin(ctx)`); repositories reach them
  through `queries.Executor(ctx)`. An outbox publish runs after `Commit`, never inside
  (docs/adr/0001).
- **Gateway route table** (`cmd/customer-gateway/.conf/gateway/{dev,test,alpha,prod}.yaml`)
  is deny-by-default and split per upstream (`customer`, `droplet`, `billing`, `notification`,
  `mail`, `storage`, `kubernetes`, selected by `X-Target-Server`; `realtime` has only the headerless socket route). A new or renamed route is not shipped until all four files carry it
  under the right service.
- **Integration gateway route table** (`cmd/integration-gateway/.conf/gateway/{dev,test,alpha,prod}.yaml`)
  fronts the services for software holding a Project API key (docs/adr/0010). Deny-by-default with
  no target header: method and path name the service, and start-up is refused when routes of two
  services could match one request. Every route lists `permissions` that `authz.IsKeyPermission`
  accepts; nothing is public or headerless. A new or renamed route is not shipped until all four
  files carry it. The gateway reads `api_key:<id>`, stamps `api_key_last_used:<id>` and holds each
  key to 100 requests a second; the service checks the key again.
- **Money** is `numeric` in PostgreSQL and `shopspring/decimal` in Go, never a float; prices
  round up, in USD, excluding taxes.
- **Config**: viper binds an env override only for keys present in the TOML, so a new key
  goes into every tracked TOML of that binary (empty is fine) in the same change.
- **Empty-string checks** go through `utils/str` (`IsEmpty`, `IsBlank`); never `== ""`.
- **Comments** explain why, never what; no narration of the edit.

## Testing

- Unit tests next to the code, one function per scenario, mockgen mocks (`make generate`);
  table-driven only for pure input matrices.
- Integration suites: `cmd/customer/integration-test` (dockertest: postgres:18, redis,
  mongo:8.2), `cmd/droplet/integration-test` (postgres:18, redis, mongo:8.2, the fake Provider,
  worker jobs called directly), `cmd/billing/integration-test` (postgres:18, redis, mongo:8.2,
  gotenberg, the fake Payment provider, a controllable clock), `cmd/notification/integration-test`
  (postgres:18, redis, consumers called directly, recording email and realtime fakes),
  `cmd/realtime/integration-test` (redis, two replicas, real WebSocket clients),
  `cmd/mail/integration-test` (postgres:18, redis, mongo:8.2, the fake MTA, a fake resolver, a
  recording Webhook receiver, the SMTP relay on a real socket, jobs and consumers called directly,
  a controllable clock), `cmd/storage/integration-test` (postgres:18, redis, mongo:8.2, the fake
  engine, jobs and consumers called directly, a controllable clock), `cmd/kubernetes/integration-test`
  (postgres:18, redis, mongo:8.2, the fake engine, jobs and consumers called directly, a controllable
  clock; the droplet suite carries the Node request and release scenarios), `cmd/customer-gateway/gateway_integration_test.go` and
  `cmd/integration-gateway/gateway_integration_test.go` (redis, a stub mail upstream); gated by
  `INTEGRATION=true`; assert the exact `HttpStatus` of the triggered rerror. The customer and
  droplet suites are the primary seams and every user-facing flow gets a scenario there; so are
  the mail suite for Mail, the storage suite for Storage and the kubernetes suite for Kubernetes. The real Cherry API is touched only by the read-only contract test
  under `NODERA_DROPLET_SMOKE`; a real KumoMTA runs the policy only in the contract test under
  `NODERA_MAIL_SMOKE`; a real Ceph runs only in the contract test under `NODERA_STORAGE_SMOKE`; a
  real Kamaji runs only in the contract test under `NODERA_KUBERNETES_SMOKE`, against the
  management cluster the `NODERA_KAMAJI_*` variables name (`deploy/kubernetes/README.md`, the
  pinned edge tag), which every move of that tag passes first.
- Tools built with this module's Go version live in `.local/bin` (`make tools`); the
  Makefile prepends it to PATH.

## Commands

`make docker-up`, `make migrate-customer` / `make migrate-droplet` / `make migrate-billing` /
`make migrate-notification` / `make migrate-mail` / `make migrate-storage` / `make migrate-kubernetes`,
`make seed-customer` / `make seed-droplet` / `make seed-mail` / `make seed-storage` /
`make seed-kubernetes`, `make create-db-mail` / `make create-db-storage` / `make create-db-kubernetes`
(an older postgres volume), `make start`
/ `make stop` / `make logs-tail`, `make test`, `make test-all`,
`make test-droplet-integration`, `make test-billing-integration`, `make test-notification-integration`,
`make test-realtime-integration`, `make test-mail-integration`, `make test-storage-integration`,
`make test-kubernetes-integration`, `make test-integration-gateway`, `make test-kumomta-contract`,
`make test-ceph-contract`, `make test-kamaji-contract`, `make lint`,
`make swagger-customer` / `make swagger-droplet` / `make swagger-notification` / `make swagger-mail` /
`make swagger-storage` / `make swagger-kubernetes`,
`make generate`. One-off: `go run ./cmd/customer replay-projects --env=dev` republishes Team and
Project events for rows that predate them; `go run ./cmd/droplet replay-catalog --env=dev`
republishes Plan and Region events for the kubernetes context's catalog read model;
`go run ./cmd/customer sync-api-keys --env=dev` rewrites
every API key into Redis from the database. Operator: `make unpause-mail PROJECT=<id>` lifts a
Project's Pause; `make pause-storage STORAGE=<id> REASON=<code>` / `make unpause-storage STORAGE=<id>`
stop and reopen a Storage. Dev only: `make sample-notification TEAM=<id> [TYPE=…|all]`
sends a sample Notification through RabbitMQ; `make sample-payment-method TEAM=<id>` saves a
Payment method no provider holds (dev has no provider credentials), which makes a Team active;
`make verify-sending-domain SENDING_DOMAIN=<id>` verifies a Sending domain as if its DNS records
were published (dev's zone is not public). Dev's KumoMTA (docker-compose) delivers into MailHog.
