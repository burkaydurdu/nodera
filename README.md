# Nodera

Self-service bare-metal cloud. A customer asks for a CPU/RAM/disk/region and gets a
**Droplet**: a physical server rented from an integrated provider and set up by the
platform. This repository is the Go backend; the web app lives in the sibling
`nodera-client` repository.

Vocabulary: [CONTEXT-MAP.md](CONTEXT-MAP.md). Decisions: [docs/adr](docs/adr). Phase 1
spec: [docs/specs](docs/specs).

## Binaries

| Binary | Purpose | Dev port |
| --- | --- | --- |
| `cmd/customer` | customer service: sign-up, sign-in, MFA, profile, teams, projects, SSH keys (`server`, `outbox`, `migrate`, `seed`, `replay-projects`) | 9650 |
| `cmd/customer-gateway` | reverse proxy in front of the customer and droplet services; `X-Target-Server` (`customer` or `droplet`) picks the upstream and the route table per upstream is the authorization source of truth | 3460 |
| `cmd/droplet` | droplet service: catalog and Droplets inside a Project (`server`, `worker`, `migrate`, `seed`); the worker runs the catalog sync, the status poller and the customer-events consumer | 9660 |
| `cmd/notification` | worker that turns email events into emails | – |

Bounded contexts live under `contexts/<name>/`; a context never imports another.

## Run it locally

Prerequisites: Go (the version in `go.mod` is downloaded automatically), Docker, `make`.

```bash
make tools                      # swag, mockgen, golangci-lint, migrate into .local/bin
cp cmd/customer/.conf/dev.toml.example cmd/customer/.conf/dev.toml
cp cmd/customer-gateway/.conf/dev.toml.example cmd/customer-gateway/.conf/dev.toml
cp cmd/notification/.conf/dev.toml.example cmd/notification/.conf/dev.toml

make docker-up                  # Postgres 18, Redis, MongoDB (outbox only), RabbitMQ, MailHog
make mongo-init                 # once: single-node replica set
make migrate-customer seed-customer
make start                      # customer server + outbox, gateway, notification worker
```

Then open the web app on <http://localhost:3002>, MailHog on <http://localhost:8025>
(every code and reset link lands there), Swagger on <http://localhost:9650/swagger/index.html>.

Seeded dev sign-in: `dev@nodera.local` / `Nodera1234!`. In `dev` every verification and
MFA code is `123456`.

Configuration is TOML per environment under `cmd/<binary>/.conf/` (`dev.toml` is
gitignored); every key can be overridden with `NODERA_<BINARY>_<SECTION>_<KEY>`.

### Customer: replaying Team and Project events

The customer service publishes `TeamCreated`, `TeamMemberAdded` and `ProjectCreated` through
its outbox so the droplet context can keep its Project access read model
([ADR-0004](docs/adr/0004-droplet-context-learns-projects-from-events.md)). Rows that
existed before those events did, or a read model rebuilt from scratch, are caught up with a
one-off replay that writes the three events for every existing Team, membership and Project
into the outbox; the `outbox` command relays them as usual. Consumers upsert, so running it
twice is harmless.

```bash
go run ./cmd/customer replay-projects --env=dev
```

### Droplet: Provider smoke test

The Provider adapters share one read-only contract suite
(`contexts/droplet/providers/contract_test.go`). It always runs against the in-memory fake;
against Cherry Servers it runs only when the environment asks for it, and it never creates
anything supplier-side:

```bash
NODERA_DROPLET_SMOKE=true \
NODERA_DROPLET_PROVIDERS_CHERRY_TOKEN=<api token> \
NODERA_DROPLET_PROVIDERS_CHERRY_PROJECT_ID=<project id, optional for read-only calls> \
NODERA_DROPLET_PROVIDERS_CHERRY_ENDPOINT=<api base url, optional> \
go test ./contexts/droplet/providers/ -run TestCherryContract_Smoke -count=1 -v
```

## Tests

```bash
make test                       # unit tests
make test-all                   # + dockertest suites (needs Docker and `migrate` on PATH)
make test-customer-integration  # the customer HTTP suite alone
make test-gateway-integration
make test-droplet-integration
make lint
```

The customer HTTP suite (`cmd/customer/integration-test`) is the primary seam: it boots
the real dependencies in containers, wires the server exactly as the binary does and
drives every flow over HTTP. Emails are observed as outbox documents.
