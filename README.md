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
| `cmd/customer-gateway` | reverse proxy in front of the customer, droplet, billing and notification services and the realtime socket; `X-Target-Server` (`customer`, `droplet`, `billing` or `notification`) picks the upstream, the headerless `/v1/realtime` route reaches the realtime service, and the route table per upstream is the authorization source of truth | 3460 |
| `cmd/droplet` | droplet service: catalog and Droplets inside a Project (`server`, `worker`, `outbox`, `migrate`, `seed`, `replay-billing`); the worker runs the catalog sync, the status poller and the customer- and billing-events consumers | 9660 |
| `cmd/notification` | Notifications (`server`, `worker`, `migrate`): the worker records a Notification for what happened and delivers it by email and In-app, and sends the platform's other emails; the server is the User's Inbox. Gateway target `notification` | 9680 |
| `cmd/realtime` | the web app's live socket (`server`): one WebSocket per tab at `/v1/realtime`, fed by the contexts' realtime messages over RabbitMQ and shared between replicas over Redis pub/sub ([ADR-0007](docs/adr/0007-live-updates-reach-realtime-over-rabbitmq.md)) | 9690 |
| `cmd/billing` | billing service: Billing profile, Payment methods, usage, Invoices, Payments, Dunning, Billing status and provider webhooks (`server`, `worker`, `outbox`, `migrate`, `seed`, `replay-status`); the worker runs the exchange-rate fetch, Period close, Threshold check, Dunning, reconciliation, the collection sweep, the card-expiry warning and the customer- and usage-events consumers. Gateway target `billing` | 9670 (metrics 9104) |

Bounded contexts live under `contexts/<name>/`; a context never imports another.

## Run it locally

Prerequisites: Go (the version in `go.mod` is downloaded automatically), Docker, `make`.

```bash
make tools                      # swag, mockgen, golangci-lint, migrate into .local/bin
cp cmd/customer/.conf/dev.toml.example cmd/customer/.conf/dev.toml
cp cmd/customer-gateway/.conf/dev.toml.example cmd/customer-gateway/.conf/dev.toml
cp cmd/notification/.conf/dev.toml.example cmd/notification/.conf/dev.toml
cp cmd/realtime/.conf/dev.toml.example cmd/realtime/.conf/dev.toml

make docker-up                  # Postgres 18, Redis, MongoDB (outbox only), RabbitMQ, MailHog on ports +10
make mongo-init                 # once: single-node replica set
make migrate-customer seed-customer
make migrate-notification       # `make create-db-notification` first on a volume older than the database
make start                      # every server, worker and outbox, the gateway and the realtime socket
```

Then open the web app on <http://localhost:3002>, MailHog on <http://localhost:8035>
(every code and reset link lands there), Swagger on <http://localhost:9650/swagger/index.html>.

Seeded dev sign-in: `dev@nodera.local` / `Nodera1234!`. In `dev` every verification and
MFA code is `123456`.

docker-compose publishes every service on its standard port plus ten (Postgres 5442, Redis 6389,
MongoDB 27027, RabbitMQ 5682 and 15682, MailHog 1035 and 8035), so Nodera runs next to other
local stacks holding the standard ports; the dev TOMLs point at these.

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

### Droplet: the meter and replaying it

The droplet service writes `DropletBillingStarted` (first time a Droplet is active, with its
Locked price) and `DropletBillingStopped` (deletion requested, or failed after having run)
into its own outbox, and its `outbox` command relays them to RabbitMQ
([ADR-0006](docs/adr/0006-billing-through-craftgate-with-an-e-invoice-ready-model.md)); run it
next to the server and the worker. Each boundary is announced once per Droplet. The worker
also consumes `TeamBillingStatusChanged` and `TeamDropletsDeletionRequested` into the Billing
status read model: no payment method refuses creation, suspension refuses creation and
power-on and powers the Team's running Droplets off (and back on at recovery), and a deletion
request deletes them. The dev seed marks the seeded Team `active`.

Droplets that ran before the events existed, or a billing context rebuilt from scratch, are
caught up with a one-off replay that writes both events for every Droplet that was ever
active; billing keeps one usage row per Droplet, so running it twice is harmless.

```bash
go run ./cmd/droplet outbox --env=dev
go run ./cmd/droplet replay-billing --env=dev
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

### Billing

The billing service ([ADR-0006](docs/adr/0006-billing-through-craftgate-with-an-e-invoice-ready-model.md),
[spec](docs/specs/2026-10-01-phase-4-billing.md)) has its own database `nodera_billing`
(created by `scripts/init-db.sql`; on an existing Postgres volume run `make reset-db-billing`
once), a Mongo outbox and RabbitMQ consumers. docker-compose carries
[Gotenberg](https://gotenberg.dev) on <http://localhost:3300>, the HTML-to-PDF renderer for
Invoice PDFs.

```bash
cp cmd/billing/.conf/dev.toml.example cmd/billing/.conf/dev.toml
make migrate-billing seed-billing   # schema, payment routes, the first two tax rules, thresholds
make server-billing                 # :9670, Swagger on /swagger/index.html
make worker-billing                 # rates, collection jobs, customer and usage consumers
make outbox-billing                 # relays the status, deletion and email events
```

Which Payment provider a billing country uses is the `payment_routes` table (seeded
`TR → craftgate, TRY`, `* → craftgate, USD`), not configuration; the provider's credentials
are configuration. With no provider block enabled, starting a card verification answers
500 31-10010, so local card flows need Craftgate sandbox keys. Every key can be overridden
with `NODERA_BILLING_<SECTION>_<KEY>`:

| Variable | Meaning |
| --- | --- |
| `NODERA_BILLING_POSTGRESQL_URI`, `NODERA_BILLING_REDIS_*`, `NODERA_BILLING_MONGODB_*`, `NODERA_BILLING_RABBITMQ_*` | infrastructure, as for the other binaries |
| `NODERA_BILLING_SECRET_JWT` | the customer service's access-token secret (verify only) |
| `NODERA_BILLING_SECRET_LINKS` | signs the Invoice PDF and payment-confirmation links in billing emails; rotating it voids every link already sent |
| `NODERA_BILLING_PROVIDERS_CRAFTGATE_ENABLED`, `_API_KEY`, `_SECRET_KEY`, `_BASE_URL`, `_WEBHOOK_KEY`, `_THREEDS_CALLBACK_KEY` | Craftgate; `base_url` empty means production, sandbox is `https://sandbox-api.craftgate.io`; the 3-D Secure callback key verifies the bank's return |
| `NODERA_BILLING_PROVIDERS_STRIPE_ENABLED`, `_SECRET_KEY`, `_WEBHOOK_SECRET` | Stripe, built but not routed |
| `NODERA_BILLING_PDF_GOTENBERG_URL` | Gotenberg (dev `http://localhost:3300`) |
| `NODERA_BILLING_STORAGE_LOCAL_DIR` | where rendered PDFs are kept in dev and tests |
| `NODERA_BILLING_EXCHANGE_TCMB_URL` | central bank bulletin; empty means `https://www.tcmb.gov.tr/kurlar/today.xml` |
| `NODERA_BILLING_JOBS_THRESHOLD_INTERVAL_MINUTES`, `_DUNNING_INTERVAL_MINUTES`, `_RECONCILE_INTERVAL_MINUTES` | worker job intervals |
| `NODERA_BILLING_DOMAIN_FRONT` | the web app origin the card form and payment links return to |
| `NODERA_BILLING_DOMAIN_GATEWAY` | the gateway's public origin; the payment provider posts the 3-D Secure result to it |
| `NODERA_BILLING_BILLING_START_AT` | the cutover (RFC 3339): usage before it is never billed |

Provider webhooks arrive at `POST /v1/billing/webhooks/craftgate` and `/stripe` (public on
the gateway); they are verified, stored once by event id and reconciled by the worker.

#### Usage, Invoices and collection

- **Usage** comes from the droplet context's `DropletBillingStarted` / `DropletBillingStopped`
  (one row per Droplet; a stop that overtakes its start is kept as a tombstone). A Droplet's
  hours in a Period are the overlap of [start, stop or now) with the UTC month, rounded up to
  whole hours and capped at 672, priced at the Locked price and rounded up to the cent.
  `GET /v1/teams/:teamID/billing/usage` is the draft Invoice: hours not yet on an Invoice.
- **Invoices** are issued per Team and Period at month end, or early when the unbilled
  amount reaches the Threshold (`thresholds` table: $50 in a Team's first three months, $500
  after). Issue applies the Tax rule of the profile's country and account type, then Credit,
  numbers it `NDR-YYYY-NNNNNN` per year, snapshots the profile and, for Turkish Teams, fixes
  the TRY amount with the issue date's central-bank rate (latest earlier one on weekends). A
  zero subtotal is not issued; Credit that covers everything issues it already paid.
- **PDF**: an HTML statement per locale (`contexts/billing/pdf/templates`, Turkish for
  Turkish Teams, English otherwise) rendered through Gotenberg and kept by the storage
  adapter (local directory in dev and tests). A failed render is retried on the first
  download. It says "statement" until an e-invoice issuer is connected (`einvoice.None`).
  The issue email's link opens `{front}/billing/invoice-pdf?invoice=&expires=&token=`, a web
  page that downloads through the gateway (a link cannot send the target header).
- **Exchange rate**: the TRY amount uses the latest central-bank bulletin dated strictly
  before the issue date, since the bank publishes in the afternoon the rates for the next
  day (to be confirmed with the accountant).
- **Payments** charge the default card in its own currency (TRY cards pay the settlement
  amount); the attempt is recorded first, with the key `invoice:{id}:attempt:{n}`. Only a
  bank decline fails an attempt: any other provider error leaves it pending. Craftgate does
  not dedupe by key, so reconciliation looks a pending attempt up by its key (after twice
  the call timeout) and adopts what it finds; it charges again only when the provider holds
  nothing. No attempt starts while another is pending or its confirmation link is open
  ("Pay now" answers 409 33-10007). "Pay now" is 5 attempts an hour per Team.
- **3-D Secure** opens only when the cardholder is there: a bank refusal marks the attempt
  `confirmation_required` and emails `{front}/billing/confirm?payment=&token=`; that page
  calls `POST /v1/billing/payments/:id/3ds/start?token=` (answers `html_content`, a decoded
  HTML document, or `redirect_url`), the provider posts the result to
  `POST /v1/billing/payments/:id/3ds/callback?token=`, which verifies the provider's hash,
  completes the charge and redirects to `{front}/billing/confirm?payment=&result=`. A link
  lapses after 7 days, which fails the attempt and starts Dunning.
- **Dunning** from the first failure F: retries at F+1d, F+3d, F+7d (an email each, skipped
  while an attempt is in progress); from F+7d the Team is suspended whatever the day-7
  attempt answered (status event and email); warnings at F+14d and F+20d; at F+21d
  `TeamDropletsDeletionRequested`, only while the Team is suspended. Any successful Payment
  ends it and lifts a suspension. A worker that missed days makes the latest retry once.
- **Billing status** changes carry strictly increasing `changed_at` per Team;
  `go run ./cmd/billing replay-status --env=dev` republishes every Team's current status.

| Worker job | Interval | Does |
| --- | --- | --- |
| exchange rate | hourly until today has a rate | stores the TCMB USD buying rate |
| close periods | 5 min | issues last Period's Invoice per Team with usage (idempotent) |
| thresholds | `jobs.threshold_interval_minutes` | issues a Threshold Invoice when crossed |
| dunning | `jobs.dunning_interval_minutes` | retries, suspends, warns, requests deletion |
| reconcile | `jobs.reconcile_interval_minutes` | settles stored webhooks and pending Payments from the provider's API |
| sweep | `jobs.reconcile_interval_minutes` | fails lapsed confirmation links; charges issued Invoices nobody charged (sending a missed issue email) |
| card expiring | daily | warns once per default card expiring within 30 days |

Emails (routing key → notification kind), addressed to the profile's invoice email and every
owner, each once and in their own Locale: `InvoiceIssued` (with the signed PDF link, valid
30 days), `PaymentSucceeded`, `PaymentFailed` (attempt and next retry),
`PaymentConfirmationRequired`, `TeamSuspended`, `DropletsDeletionWarning`, `CardExpiring`.
Events for the droplet context: `TeamBillingStatusChanged`, `TeamDropletsDeletionRequested`.

Public routes besides the webhooks (no session; the link token authorizes):
`GET /v1/billing/payments/:paymentID/confirm?token=` (the attempt's status, 410 34-10002 for
a bad or expired token), `POST /v1/billing/payments/:paymentID/3ds/start?token=`,
`POST /v1/billing/payments/:paymentID/3ds/callback?token=` (posted by the provider; 302 to
the web app) and `GET /v1/billing/invoices/:invoiceID/pdf?expires=&token=` (the PDF link's
download, 410 33-10005 for a bad or expired link).

## Tests

```bash
make test                       # unit tests
make test-all                   # + dockertest suites (needs Docker and `migrate` on PATH)
make test-customer-integration  # the customer HTTP suite alone
make test-gateway-integration
make test-droplet-integration
make test-billing-integration   # also starts Gotenberg in a container
make lint
```

The customer HTTP suite (`cmd/customer/integration-test`) is the primary seam: it boots
the real dependencies in containers, wires the server exactly as the binary does and
drives every flow over HTTP. Emails are observed as outbox documents.

### Notifications and the live socket

A Notification is recorded for good and delivered over the Channels its type fixes
([contexts/notification/CONTEXT.md](contexts/notification/CONTEXT.md)). The notification
worker keeps its own Team read model from the customer events and tells the members whose Role
may see what happened ([ADR-0008](docs/adr/0008-the-notification-context-decides-who-is-told.md)):
a Droplet ready or failed, every billing event (to `billing.read` and the profile's invoice
address), an invitation, a role change or removal someone else made, and the sign-in mail (by email
only, its code or link never recorded). Delivery is tracked per Channel, so a redelivery from the
error queue sends nothing twice.

The web app opens one WebSocket per tab at `ws://localhost:3460/v1/realtime` through the
gateway and sends `{"type":"auth","token":"<access token>"}` as its first frame, and again
after every token refresh; the server answers `{"type":"ready"}` and then pushes
`{"type","data","at"}` messages. It closes with `4401` when the session ended or the token's
permission version is stale (refresh, then reconnect) and with `4408` when the first frame was
late. Contexts publish realtime messages through their outbox like any event; the outbox relays
poll every second for that.

To see one arrive without creating a real Droplet (dev's Provider is real and bills), send a
sample: it publishes, signed, the event behind a Notification about a Team, and everything from the
worker to the web app's toast runs as for the real thing. The Team id is on the web app's Team page,
next to the title. The sample names a Droplet or an Invoice that does not exist, so opening it finds
nothing.

```bash
make sample-notification TEAM=<team id>                   # droplet_active
make sample-notification TEAM=<team id> TYPE=payment_failed
make sample-notification TEAM=<team id> TYPE=all           # every Notification type about a Team
```

Memberships that existed before the notification context did reach its read model with the
customer replay; start the notification worker first so its queues exist.

```bash
make worker-notification
go run ./cmd/customer replay-projects --env=dev
```
