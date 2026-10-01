# Phase 2: Droplets — catalog, pricing with Commission, and the first Provider

Vocabulary: `contexts/droplet/CONTEXT.md` and `contexts/customer/CONTEXT.md`. Decisions already
recorded: ADR-0001 (MongoDB only for the outbox), ADR-0002 (sessions), ADR-0003 (a Provider must
bill hourly and permit resale; Cherry Servers first), ADR-0004 (the droplet context learns
Projects from events).

## Problem Statement

A signed-in User has a Team and a Project but nothing to put in them. Nodera cannot yet show
what a Droplet costs or where it can run, cannot create one, and cannot tell a customer whether
the machine they asked for is up. Behind the scenes there is no Provider integration, no notion
of the price Nodera pays versus the price it charges, and no record from which usage could later
be billed.

## Solution

The first slice of the product itself, built on a new **droplet** bounded context:

- A **catalog** the customer browses: Regions (city and country), Plans (CPU, RAM, disk,
  included bandwidth, one public IPv4) with an hourly price in USD excluding taxes and the
  monthly amount it caps at, and the Images a Droplet can be installed with. Sold-out Plans stay
  visible and are marked as such.
- **Pricing with Commission**: every Plan is backed by exactly one Provider Offer per Region;
  its price is the Offer's hourly price plus Nodera's Commission, nothing else. Commission is a
  percentage, defaulted per Provider and overridable per Plan, kept in tables so an admin surface
  can manage it later. The price a Droplet was created at is locked onto it for life.
- **Droplets inside a Project**: create (Plan, Region, Image, hostname, Team SSH keys), list,
  detail with IPs and status, power on/off/reboot, delete. A status history is kept so the
  billing phase can compute hours of usage from it.
- **Providers**: an adapter interface drawn against Cherry Servers and Zenlayer's pay-as-you-go
  API; Cherry Servers implemented now, a fake adapter for tests, a catalog sync every 15 minutes
  and a status poller, both as jobs of a droplet worker.
- **Team SSH keys** in the customer context, and the customer context's Team/Project events so
  the droplet service can authorize requests locally (ADR-0004).
- A **droplet route table** in the customer gateway, which now fronts two services.
- The **web app**: a catalog page, a create-Droplet flow inside a Project, the Project page
  becoming its Droplet list, a Droplet detail page, and an SSH keys page under the Team.

## User Stories

### Catalog and prices

1. As a User, I want to see the Regions Nodera offers as a city and country, so that I can pick
   where my Droplet runs without caring who operates the datacenter.
2. As a User, I want to see the Plans available in a Region with CPU, RAM, disk and included
   bandwidth, so that I can size my Droplet.
3. As a User, I want each Plan to show an hourly price and the monthly amount it caps at, so that
   I know both what a short experiment and a full month will cost.
4. As a User, I want prices labelled as excluding taxes in USD, so that I am not surprised at
   payment time.
5. As a User, I want a sold-out Plan to stay listed and be marked sold out, so that I know it
   exists and can come back for it rather than wonder whether it was removed.
6. As a User, I want to see the Images a Plan can be installed with, so that I can choose my
   operating system.
7. As a User, I want the catalog to load without being signed in to a particular Team, so that I
   can compare prices before deciding where to create a Droplet.
8. As Nodera, I want a Plan's price to be exactly the Provider's hourly price plus my Commission,
   so that pricing is mechanical, auditable and never hand-typed.
9. As Nodera, I want a default Commission per Provider and an optional override per Plan, so that
   I can price a headline Plan differently without touching the rest.
10. As Nodera, I want Plan prices to follow Provider price changes at the next sync, so that a
    Provider increase never eats my Commission.
11. As Nodera, I want Provider names to appear nowhere in the customer-facing product, so that
    customers know Nodera only.

### Creating a Droplet

12. As a User, I want to create a Droplet inside a Project by choosing a Region, a Plan, an Image,
    a hostname and one or more of my Team's SSH keys, so that I get a server I can log in to.
13. As a User, I want the hostname validated live, so that I learn the rules before submitting.
14. As a User, I want to be required to choose at least one SSH key, so that my Droplet is never
    created with password-only access.
15. As a User, I want the create form to show the price I will pay per hour and per month for my
    selection, so that I confirm the cost knowingly.
16. As a User, I want to be told at creation time if the Plan just sold out in that Region, so
    that I can pick another rather than wait for a failure.
17. As a User, I want to be told if the price changed since I loaded the page, so that I never
    pay more than I saw.
18. As a User, I want a Droplet whose Provider price dropped since I loaded the page to still be
    created at the price I saw, so that the promise "never more than shown" is simple and one-way.
19. As a User, I want the new Droplet to appear at once in the Project's list as provisioning, so
    that I know the request was accepted.
20. As a User, I want the Droplet to turn active with its IPv4 (and IPv6 when the Provider gives
    one) without refreshing the page, so that I can connect as soon as it is ready.
21. As a User, I want a Droplet the Provider could not deliver to show as failed with a reason I
    can act on, so that I can retry or choose another Plan.
22. As a User, I want a failed Droplet to cost nothing, so that Provider problems are not my bill.
23. As a User, I want the price my Droplet was created at to stay fixed for its whole life, so
    that catalog changes never change my running costs.

### Living with Droplets

24. As a User, I want to see the Droplets of the Project I am in, with status, Region, Plan,
    IPv4 and created time, so that I know what is running.
25. As a User, I want to switch Team or Project and see that Project's Droplets, so that the list
    always matches where I am.
26. As a User, I want a Droplet detail page with its IPs, Image, Plan, Region, locked hourly
    price, SSH keys it was created with and a status timeline, so that I have the full picture in
    one place.
27. As a User, I want to copy an IP address with one click, so that I can connect quickly.
28. As a User, I want to power a Droplet off, on and reboot it, so that I can recover a stuck
    machine.
29. As a User, I want the power-off dialog to remind me that a powered-off Droplet is still
    rented and billed, so that I do not expect savings from turning it off.
30. As a User, I want to delete a Droplet after a confirmation that names it, so that I never
    delete the wrong one.
31. As a User, I want billing for a Droplet to stop the moment I ask for deletion, even if the
    Provider takes a while to release it, so that Provider slowness is not my cost.
32. As a User, I want a deleted Droplet to disappear from the list, so that the list shows only
    what exists.
33. As a User, I want actions that cannot apply to the current status (rebooting a provisioning
    Droplet, deleting a deleting one) to be refused clearly, so that I understand what is possible.
34. As a member of a Team, I want to see and act on Droplets only in Projects of my Teams, so that
    Teams are isolated from one another.
35. As a User who just created a Project, I want to be able to create a Droplet in it right away,
    even if the platform needs a moment to learn about the new Project, so that the first-run
    flow never dead-ends.

### SSH keys

36. As a User, I want to add an SSH public key to my Team with a name, so that Droplets can be
    created with it.
37. As a User, I want an invalid or duplicate key to be rejected with a clear message, so that I
    fix it before creating a Droplet.
38. As a User, I want to see my Team's keys with their fingerprints, so that I can tell them apart.
39. As a User, I want to delete a key from the Team, so that keys of people who left stop being
    offered.
40. As a User, I want deleting a key not to touch Droplets already created with it, so that
    removing a key is never a destructive act on servers.

### Operating Nodera

41. As Nodera, I want a worker that syncs Offers from every enabled Provider every 15 minutes,
    so that prices and stock are fresh without deploys.
42. As Nodera, I want the sync to recompute Plan prices and sold-out flags from the Offers, so
    that the catalog follows the Provider automatically.
43. As Nodera, I want Offers a Provider lists that I have not mapped to a Plan to be stored but
    not sold, so that deciding what to sell stays a product decision.
44. As Nodera, I want the mapping of Regions, Plans and Images to Provider references to live in
    tables with a seed for launch, so that an admin surface can edit them later without a redeploy.
45. As Nodera, I want a status poller that asks the Provider about Droplets in transitional
    statuses, so that customers see status changes without webhooks.
46. As Nodera, I want each Provider's credentials in the droplet binary's configuration and
    environment, never in a table, so that secrets follow the usual secret path.
47. As Nodera, I want a Provider that runs out of prepaid balance or refuses an order to surface
    as a failed Droplet with the Provider's reason in the log and a customer-safe reason in the
    product, so that I learn about it and the customer is not exposed to Provider details.
48. As Nodera, I want the Droplet's status history kept with timestamps, so that the billing phase
    can compute usage as hours between active and deletion.
49. As Nodera, I want one command to replay Team and Project events for rows created before the
    events existed, so that the droplet service can be introduced into an existing database.
50. As Nodera, I want an optional read-only smoke test against the real Cherry Servers API, so
    that I can confirm credentials and API compatibility without spending money.
51. As Nodera, I want a second Provider to be an additional adapter behind the same interface, so
    that adding Zenlayer when it grants pay-as-you-go is not a redesign.

### Gateway and routing

52. As Nodera, I want the customer gateway to forward droplet routes to the droplet service and
    customer routes to the customer service, so that customers use one host.
53. As Nodera, I want droplet routes to be deny-by-default exactly like customer routes, so that
    an unlisted droplet path does not exist.
54. As a User, I want catalog routes to require a session like everything else behind the gateway,
    so that pricing is visible to signed-in customers only.

## Implementation Decisions

### The droplet context

- A new bounded context, `droplet`, with its own PostgreSQL database (`nodera_droplet`) and
  Redis (rate limits and the sync/poller locks). It consumes customer events over RabbitMQ and
  emits none this phase, so it has no outbox yet; one is added (ADR-0001) with its first event.
  It never imports the customer context. It knows Teams and Projects only through its
  Project access read model (ADR-0004).
- Aggregates: `provider` (Provider records, default Commission), `catalog` (Regions, Images,
  Offers, Plans and their Provider mappings), `droplet` (Droplets, status history), `access`
  (Project access read model). Handlers orchestrate; `api/v1` binds and validates; `dto` answers.
- Binaries: `droplet` with cobra commands `server`, `worker`, `migrate`, `seed`. The worker runs
  three jobs in one process: catalog sync (every 15 minutes), status poller (every 30 seconds),
  and the consumer of customer events. Each job takes a Redis lock so two replicas never sync
  twice. The worker's job functions are callable once, synchronously, so tests drive them
  directly.
- Configuration follows the customer binary: TOML per environment with env overrides under
  `NODERA_DROPLET_*`; a `[providers.cherry]` block with `enabled`, `token`, `project_id` (the
  single Cherry project all Droplets are created in) and `endpoint`; intervals for sync and
  polling. The read-only smoke test is switched on by an environment variable, never by a
  config key, so no binary can run it by accident.

### Provider adapter

- One Go interface per Provider, implemented by `cherry` (over the official Go SDK) and `fake`
  (in-memory, scripted). Operations: list Provider regions; list Provider plans with, per region,
  the hourly USD price and stock quantity; list Provider images; ensure an SSH public key exists
  Provider-side and return its reference (matched by fingerprint); create a server (plan, region,
  image, hostname, key references, hourly cycle) returning the Provider reference; get a server
  (Provider state, IPv4/IPv6); power action (on, off, reboot); delete a server.
- Cherry Servers specifics stay inside its adapter: `cycle` is always `hourly`; the Provider
  state and status strings map to Droplet statuses in one table; stock comes from the plan's
  per-region stock quantity; provider errors are wrapped into `provider` domain rerrors with a
  customer-safe message and the raw message logged.
- Reinstall and rescue exist on the interface as optional capabilities left unimplemented this
  phase.

### Catalog, Offers, Plans, Commission

- Region: Nodera code (`fra1`, `ams1`, `vno1`, `sto1`, `sgp1`, `tyo1`, `chi1` for Cherry's
  Frankfurt, Amsterdam, Šiauliai, Stockholm, Singapore, Tokyo, Chicago; Chicago is in because it
  holds most of Cherry's hourly stock), city, ISO country, enabled flag. Mapped to a
  Provider region reference per Provider.
- Image: Nodera code, display name, family and version; launch set `ubuntu-24-04`,
  `ubuntu-22-04`, `debian-12`, each mapped to a Provider image reference.
- Offer: Provider, Provider plan reference, Provider region reference, hourly USD price, stock,
  the raw hardware description, synced-at. Upserted by the sync; unmapped Offers are kept and not
  sold.
- Plan: Nodera code (such as `c1-6-48`), Region, the single Offer it is backed by, CPU count and
  description, RAM GB, disk description, included bandwidth, Commission override in basis points
  (nullable), computed hourly price, monthly cap hours (672), sold-out flag, enabled flag.
- Commission is stored in basis points. Effective commission = Plan override if present, else the
  Provider default. Price = Offer hourly price × (1 + commission), rounded **up** to 4 decimals.
  Monthly amount shown = hourly price × 672, rounded up to cents. Prices are `numeric`, never
  floats, and are always net of taxes in USD.
- The sync recomputes every mapped Plan's price and sold-out flag from its Offer after each
  fetch; a change is recorded with the old and new price in the log. A Plan whose Offer
  disappears from the Provider is marked sold out, never deleted.
- Launch catalog is a seed migration: Cherry Provider with a default Commission, the seven Regions,
  three Images and a first set of Plans mapped to Cherry plan slugs. The exact slugs are read from
  the live Cherry catalog during implementation and recorded in the seed.

### Droplets

- Droplet: id (UUID v7), Project id, Team id (copied from Project access at creation for cheap
  listing), Plan id plus a snapshot of Plan code, Region, Image, hostname, **locked hourly
  price**, status, Provider, Provider reference (nullable until the Provider answers), IPv4,
  IPv6, the SSH key names and fingerprints it was created with, created-by User id, created-at,
  active-at, deletion-requested-at, deleted-at, failure reason.
- Status history: one row per transition with from, to, at, and a reason; usage is the sum of
  intervals where the status is `active` or `off`, from the first `active` until deletion was
  requested.
- Create flow: validate request (hostname RFC 1123 label 1–63 chars, 1–10 keys, Plan enabled and
  not sold out, Image enabled and mapped for the Plan's Provider), check Project access for the
  caller, then ask the Provider live for the Plan's current price and stock in that Region. Stock
  zero → `plan sold out` (409) and the Plan is flagged. Provider price above the Offer price the
  Plan was computed from → `plan price changed` (409) and a resync is scheduled. Otherwise the
  Droplet is inserted as `provisioning` with the locked price, the transaction commits, and only
  then is the Provider called. Provider success stores the reference; Provider failure moves the
  Droplet to `failed` with the reason. The customer sees 201 with the provisioning Droplet either
  way; the outcome arrives through the list/detail.
- Poller: every 30 seconds, for Droplets in `provisioning`, `deleting`, or with a pending power
  action, ask the Provider and apply the mapped status; on `active` for the first time set
  active-at and store IPs. A Droplet provisioning for more than 60 minutes becomes `failed`.
- Power actions are requests recorded on the Droplet and forwarded to the Provider; the resulting
  status comes from the poller. Allowed from `active` and `off` only.
- Delete: allowed from every status but `deleting` and `deleted` (a customer may abandon a
  Droplet still provisioning). Sets deletion-requested-at and status `deleting`, calls the
  Provider; the poller retries the Provider delete until the server is gone, then moves it to
  `deleted`. Rows are never hard-deleted. A `deleting` or `deleted` Droplet cannot be acted on
  again.
- A Droplet still provisioning after 60 minutes is failed for the customer and its server, if
  the Provider ever created one, is released best-effort so Nodera does not keep paying for it.
- Every read-modify-write on a Droplet (user action, delete, poller step) runs in a transaction
  with a row lock, so the poller and a user action cannot overwrite each other's changes.
- Requests carry the SSH keys' names and public key text as selected in the web app; the droplet
  service verifies each is a parseable OpenSSH public key and computes fingerprints itself. It
  never reads the customer database.

### Project access (ADR-0004)

- The customer context adds `team.created` (team id, owner user id), `team.member_added` (team
  id, user id, role) and `project.created` (project id, team id) to its outbox event types and
  publishes them after the commits that create those rows. The customer binary gains a
  `replay-projects` command that republishes these for all existing rows.
- The droplet worker consumes them into `project_access` (project id → team id) and
  `team_access` (team id, user id). Authorization for every Droplet route is: Project known, and
  the caller a member of its Team; otherwise `project not found` (404), never 403, so Project ids
  are not enumerable.
- The web app, after creating a Project, retries the Project's Droplet list for up to five
  seconds while it answers `project not found`, then shows the create-Droplet call to action.

### API contracts (droplet service, under the gateway)

- `GET /v1/catalog/regions` → enabled Regions with code, city, country and the number of
  Plans available.
- `GET /v1/catalog/plans?region=` → enabled Plans of that Region with hardware, hourly price,
  monthly amount, sold-out flag and the Image codes offered.
- `GET /v1/catalog/images` → enabled Images.
- `GET /v1/projects/:projectID/droplets` → Droplets not `deleted`, newest first.
- `POST /v1/projects/:projectID/droplets` `{plan, region, image, hostname, ssh_keys:[{name,
  public_key}]}` → 201 Droplet.
- `GET /v1/droplets/:dropletID` → Droplet with status history.
- `POST /v1/droplets/:dropletID/actions` `{type: power_on|power_off|reboot}` → 202.
- `DELETE /v1/droplets/:dropletID` → 202 Droplet as `deleting`.
- All routes require a session. Rate limits per User on create (10 per 10 minutes) and actions
  (30 per 10 minutes).
- Error domains for the droplet context are new and repo-wide unique (catalog, droplet,
  provider, access); each rerror has an explicit HTTP status.

### Customer service additions

- Aggregate `sshkey`: id, Team id, name (1–64), public key text, fingerprint (SHA-256, unique per
  Team), created-by, created-at. Routes `GET/POST /v1/teams/:teamID/ssh-keys`,
  `DELETE /v1/teams/:teamID/ssh-keys/:keyID`, owner-only like the Team routes. Public keys are
  parsed with the Go SSH library; `ssh-ed25519` and `ssh-rsa` (≥ 2048 bits) accepted.
- Outbox events above, and the `replay-projects` command.

### Gateway

- The gateway configuration gains a second upstream, `droplet`, selected by
  `X-Target-Server: droplet`; the YAML route table lists droplet routes in all four environments.
  Session checking is unchanged. A route in the table for one upstream is not valid for the
  other.

### Web app (nodera-client)

- Catalog page (`/dashboard/catalog`): Region cards, Plan table per Region with hourly and
  monthly prices, "excl. taxes" label from locale files, sold-out badge; Image chips.
- Project page becomes the Droplet list with status badges, polling every 10 seconds while any
  Droplet is transitional; empty state leads to create.
- Create Droplet (`/dashboard/projects/[id]/droplets/new`): Region → Plan → Image → hostname →
  SSH keys (multi-select from the Team's keys, with an inline "add key" modal) → price summary →
  create. Handles `plan sold out` and `plan price changed` by refreshing the catalog and
  explaining.
- Droplet detail: IPs with copy, status timeline, power actions with the billing reminder on
  power-off, delete with a name-confirming dialog.
- Team SSH keys page under Team settings.
- API modules for catalog, droplets and SSH keys; all requests carry the target-server header
  for the right upstream; `api/client` selects the header from the path prefix.
- New tokens only for status colors, added to the single palette file. All strings in both
  locale files.

## Testing Decisions

A good test drives a public interface the way a client would and asserts on what the client
sees: HTTP status, response body, a Provider-side effect visible through the fake Provider's
public state, or a row the next request returns. It never inspects handlers or repositories
directly.

Seams, highest first:

1. **Droplet integration suite** (new, mirrors the customer suite): dockertest PostgreSQL 18,
   Redis and Mongo 8.2; the droplet HTTP server built through its `BuildServer` with the **fake
   Provider**; the worker's sync, poll and consume functions called once per step. Scenarios
   cover the whole catalog (prices from Offer + Commission, override, rounding, sold out, Offer
   disappears), every Droplet flow (create → poll → active with IPs; sold out at create; price
   up rejected; price down accepted at shown price; Provider failure → failed; power actions
   from allowed and disallowed statuses; delete stops the meter at request time; deleted hidden),
   Project access (unknown Project 404; member of another Team 404; consumer makes a Project
   known), and rate limits. The fake Provider is scripted per scenario (stock, price, failure,
   delay to active).
2. **Customer integration suite** (existing): SSH key create/list/delete, invalid and duplicate
   keys, owner-only; outbox documents for `team.created`, `project.created` and
   `team.member_added`; the `replay-projects` command's effect on the outbox.
3. **Gateway integration suite** (existing): a droplet route forwards to the droplet upstream
   with the session check; an unlisted droplet path is 404; a customer path with the droplet
   target is 404.
4. **Provider adapter contract test**: one table of read-only expectations (regions non-empty,
   plans carry hourly USD prices and stock, images include the mapped codes) run against the fake
   always and against Cherry Servers when the smoke-test env is set.

Unit tests for pure logic only: price computation and rounding (table-driven), status mapping
from Cherry states, the Droplet status transition table, SSH public key parsing and fingerprint.

Prior art: `cmd/customer/integration-test` (suite structure, dockertest, Redis helpers),
`cmd/customer-gateway/gateway_integration_test.go`, the lockout and otp unit tests.

## Out of Scope

- Payments, invoices, taxes, currency display other than USD, and the usage meter itself. This
  phase records what those need (locked price, status history) and nothing more.
- Zenlayer and any second Provider adapter; the interface is drawn for them, not implemented.
- Bandwidth upgrades, traffic packages, additional IPs, private networking, block storage,
  backups, snapshots, reinstall, rescue mode, console access.
- Windows, ESXi or custom iPXE Images.
- Emails about Droplet events (ready, failed).
- Provider webhooks; polling is the only status source.
- Team invitations and Roles other than `owner`; member events beyond the owner are published so
  the read model is ready, but nothing creates them yet.
- Admin surface for Commission and mappings; tables and seed only.
- Istanbul and other Regions Cherry Servers does not offer.

## Further Notes

- Cherry Servers charges hourly from a prepaid balance. Keep it funded; an empty balance shows as
  a failed Droplet with a Provider reason in the log.
- The Zenlayer adapter is unblocked the day written pay-as-you-go and resale consent arrive
  (ADR-0003). Its `POSTPAID` charge type and `DescribeAvailableResources` map onto the same
  interface.
- Provider names never reach the customer: not in dto fields, error messages, hostnames, image
  names, or the web app. IP whois will reveal the datacenter operator; that is accepted and not
  hidden.
- The hourly price is the only price. A cheaper committed term a Provider may offer is a Nodera
  purchasing choice behind the same Plan, never a customer-facing cycle.
