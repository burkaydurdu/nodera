# CLAUDE.md

Nodera backend (Go, DDD). Bounded contexts under `contexts/` (customer, droplet, gateway,
notification); binaries under `cmd/`. Vocabulary in `CONTEXT-MAP.md` and the per-context
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
  `contexts/notification/events`, `contexts/droplet/async/events`). The droplet context knows
  Projects only through its event-fed read model (docs/adr/0004); it never queries customer.
- **Providers** (Cherry Servers today) are adapters behind `contexts/droplet/providers`; a
  Provider must bill hourly and permit resale (docs/adr/0003). Provider names never reach a
  DTO, an error message or the web app.
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
  (`error.go`) and an explicit `HttpStatus`. Taken domains: customer 1–9, 12; droplet
  20–23; common 100. Never mutate a var: `.Copy()` first. Sign-in
  answers `user.ErrorNotFound` for a wrong email and a wrong password alike.
- **Transactions** ride the context (`transactions.Begin(ctx)`); repositories reach them
  through `queries.Executor(ctx)`. An outbox publish runs after `Commit`, never inside
  (docs/adr/0001).
- **Gateway route table** (`cmd/customer-gateway/.conf/gateway/{dev,test,alpha,prod}.yaml`)
  is deny-by-default and split per upstream (`customer`, `droplet`; selected by
  `X-Target-Server`). A new or renamed route is not shipped until all four files carry it
  under the right service.
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
  mongo:8.2), `cmd/droplet/integration-test` (postgres:18, redis, the fake Provider, worker
  jobs called directly) and `cmd/customer-gateway/gateway_integration_test.go`; gated by
  `INTEGRATION=true`; assert the exact `HttpStatus` of the triggered rerror. The customer and
  droplet suites are the primary seams and every user-facing flow gets a scenario there. The
  real Cherry API is touched only by the read-only contract test under `NODERA_DROPLET_SMOKE`.
- Tools built with this module's Go version live in `.local/bin` (`make tools`); the
  Makefile prepends it to PATH.

## Commands

`make docker-up`, `make migrate-customer` / `make migrate-droplet`, `make seed-customer` /
`make seed-droplet`, `make start` / `make stop` / `make logs-tail`, `make test`, `make test-all`,
`make test-droplet-integration`, `make lint`, `make swagger-customer` / `make swagger-droplet`,
`make generate`. One-off: `go run ./cmd/customer replay-projects --env=dev` republishes Team and
Project events for rows that predate them.
