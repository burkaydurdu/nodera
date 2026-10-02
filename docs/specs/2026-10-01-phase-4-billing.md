# Phase 4: Billing — profile, payment methods, usage, invoices and collection

Vocabulary: `contexts/billing/CONTEXT.md` (Billing profile, Payment method, Payment provider,
Billing status, Usage, Period, Invoice, Invoice line, Threshold, Payment, Dunning, Credit, Tax
rule, Exchange rate), `contexts/droplet/CONTEXT.md` (Locked price, Droplet status),
`contexts/customer/CONTEXT.md` (Role, Permission). Decisions already recorded: ADR-0001
(outbox), ADR-0004 (read models from events), ADR-0005 (Roles in the token), ADR-0006 (billing
through Craftgate with an e-invoice-ready model).

## Problem Statement

Nodera rents servers it has paid for in advance and gives them to customers for free. A Team
can create Droplets without any way to pay, Nodera has no record of what a Team owes, no card
to charge, no invoice to send, and no way to stop a Team that does not pay. The finance Role
exists but has nothing to do.

## Solution

A **billing** bounded context that makes a Team pay for its hours:

- A **Billing profile** per Team and saved **Payment methods**, added through the Payment
  provider's hosted card verification so Nodera never sees a card number. A Team cannot create
  a Droplet until it holds one.
- **Usage** metered from the droplet context's events, priced at each Droplet's Locked price,
  capped at 672 hours per Droplet per Period.
- A **draft Invoice per Period** the Team can watch grow, **issued and charged** at month end or
  when a Threshold is crossed, with Tax lines from operator-managed Tax rules, Credit applied
  first, and for Turkish Teams the TRY equivalent at the issue-date Exchange rate.
- **Collection** through Craftgate (TRY and USD cards) behind a provider interface ready for
  Stripe, with a confirmation link when the bank insists on 3-D Secure, and a fixed **Dunning**
  schedule that retries, suspends and finally deletes the Droplets of a Team that does not pay.
- **Invoice PDFs** in the Team's language, plus emails for every billing event.
- A **Billing area** in the web app for the finance and owner Roles.

## User Stories

### Billing profile

1. As an owner or finance member, I want to fill in the Team's Billing profile (account type,
   legal name, country, address, tax identifiers, invoice email), so that invoices are made out
   correctly.
2. As a Turkish company, I want to be asked for my VKN and tax office, so that my invoices can
   become legal e-invoices later without re-entering anything.
3. As a Turkish individual, I want to be asked for my TCKN, so that the same holds for me.
4. As an owner or finance member, I want to edit the profile later, so that a move or a rename
   is reflected on the next Invoice.
5. As a Team, I want the profile to be required before a card can be added, so that no card is
   ever charged against an unknown payer.
6. As Nodera, I want a change of billing country to require a new Payment method, so that a
   card saved for one Payment provider is never charged by another.

### Payment methods

7. As an owner or finance member, I want to add a card through a secure hosted form, so that my
   card number never passes through Nodera.
8. As an owner or finance member, I want the card verified with 3-D Secure when I add it, so
   that later monthly charges can run without interrupting me.
9. As an owner or finance member, I want to see my saved cards as brand, last four digits and
   expiry, so that I can tell them apart.
10. As an owner or finance member, I want to hold several cards and pick a default, so that a
    replacement card is ready before the old one expires.
11. As an owner or finance member, I want to delete a card that is not the default, so that
    stale cards disappear.
12. As Nodera, I want the last card to be undeletable while an Invoice is unpaid, so that a
    Team cannot walk away from a debt by removing its card.
13. As a cardholder, I want an email 30 days before my default card expires, so that I replace
    it before a charge fails.
14. As a developer in a Team without a card, I want Droplet creation to tell me a Payment method
    is required and whom to ask, so that I know why I am blocked.

### Usage and the draft Invoice

15. As a member with `billing.read`, I want to see this Period's usage so far per Droplet (hours,
    hourly price, amount) and the estimated total, so that there are no surprises at month end.
16. As a Team, I want a Droplet's hours to start when it becomes active and stop the moment I ask
    for deletion, so that provisioning time and Provider slowness are not on my bill.
17. As a Team, I want a powered-off Droplet to keep counting, so that the rule I was told at
    power-off holds.
18. As a Team, I want each Droplet capped at 672 hours a Period, so that a full month costs what
    the catalog's monthly price said.
19. As a Team, I want partial hours rounded up to the next hour, so that the rule is simple and
    stated.
20. As a Team, I want a Droplet created at one price to be billed at that price for life, so
    that catalog changes never change my bill.

### Issuing and taxes

21. As a Team, I want an Invoice issued at the end of each calendar month (UTC) for that month's
    usage, so that billing is predictable.
22. As a new Team, I want an early Invoice when my usage in a Period passes $50 (during the
    first three months), so that I can verify billing early and Nodera carries less risk.
23. As an established Team, I want the early-invoice Threshold to be $500, so that mid-month
    charges are rare.
24. As a Team, I want the Invoice to list one line per Droplet with hostname, Plan, hours,
    hourly price and amount, so that I can reconcile it against my Project.
25. As a Turkish Team, I want 20% KDV shown as its own line, so that the tax is explicit.
26. As a non-Turkish Team, I want 0% tax with the export-service exemption label, so that I know
    why no tax was charged.
27. As a Turkish Team, I want the Invoice to show the USD total, the TRY equivalent and the
    central bank rate of the issue date, so that the amount charged to my TRY card is explained.
28. As a Team with Credit, I want Credit applied before my card is charged, so that prepaid
    money is used first.
29. As a Team, I want a sequential Invoice number per year, so that I can file invoices in order.
30. As Nodera, I want Tax rules kept as operator-managed data by country and account type, so
    that rates and exemptions change without a deploy.
31. As Nodera, I want an Invoice with a zero total (no usage) not to be issued, so that empty
    statements are not sent.

### Collection and Dunning

32. As a Team, I want the issued Invoice charged automatically to my default card, so that I do
    nothing month to month.
33. As a Team, I want an email with the Invoice PDF when it is issued and a receipt when it is
    paid, so that accounting has what it needs.
34. As a cardholder whose bank requires confirmation, I want an email with a link where I
    confirm the charge, so that a 3-D Secure demand does not become a failed payment.
35. As a Team whose charge failed, I want retries on days 1, 3 and 7 with an email each time,
    so that a temporary problem fixes itself.
36. As a Team whose charge still fails on day 7, I want my Droplets powered off and creation
    blocked, with a clear banner and email, so that I understand the state and the cost stops.
37. As a suspended Team, I want to pay the open Invoice from the app (retry now, or with another
    card) and have my Droplets powered back on, so that recovery is self-service.
38. As a Team still unpaid on day 14 and day 20, I want warnings that my Droplets will be deleted
    on day 21, so that data loss never comes without notice.
39. As Nodera, I want the Droplets of a Team unpaid on day 21 deleted, so that Nodera stops
    paying the Provider for them.
40. As Nodera, I want every Payment attempt recorded with the provider's result, so that disputes
    can be traced.
41. As Nodera, I want provider webhooks verified by signature, deduplicated, and reconciled
    against the provider's API rather than trusted blindly, so that a replayed or forged event
    cannot mark an Invoice paid.
42. As Nodera, I want a Payment to be idempotent per Invoice attempt, so that a retry never
    charges a Team twice.

### Billing status and the droplet context

43. As a Team, I want creating a Droplet refused with "payment method required" while I have no
    card, so that I cannot incur costs I cannot pay.
44. As a suspended Team, I want creation and power-on refused with "team suspended", so that the
    state is enforced, not just displayed.
45. As Nodera, I want the droplet context to learn Billing status from events, so that it never
    calls the billing service on a request.

### Web app

46. As a member with `billing.read`, I want a Billing area under the Team with tabs Profile,
    Payment methods, Invoices and Usage, so that money matters live in one place.
47. As a member with `billing.read` only, I want the write actions hidden, so that the UI matches
    my Role.
48. As a Team, I want the Invoices tab to show number, Period, status, totals and a PDF download,
    so that past invoices are always reachable.
49. As a Team, I want a payment-failed or suspended banner across the dashboard with a "Pay now"
    action, so that the way out is one click.
50. As a Team, I want all of it in Turkish and English, so that finance reads it in its Locale.

### Operating Nodera

51. As Nodera, I want a worker that closes Periods, issues and charges Invoices, runs Dunning and
    fetches the daily Exchange rate, so that billing runs without a person.
52. As Nodera, I want the Craftgate and Stripe credentials in the billing binary's configuration,
    so that secrets follow the usual path and the provider is swappable per country.
53. As Nodera, I want an e-invoice issuer adapter interface with a `none` implementation now, so
    that connecting an integrator later changes no model.
54. As Nodera, I want a fake Payment provider and a fake PDF renderer for tests, so that the suite
    is deterministic and free.

## Implementation Decisions

### The billing context

- New bounded context `billing` with its own PostgreSQL database (`nodera_billing`), Redis
  (rate limits, job locks), a Mongo outbox (ADR-0001) because it emits events, and RabbitMQ
  consumers for customer and droplet events. It never imports another context.
- Aggregates: `profile`, `paymentmethod`, `usage`, `invoice` (Invoice, Invoice line, Tax line,
  Credit application, numbering), `payment`, `taxrule`, `exchangerate`, `access` (Team/member
  read model from customer events, same shape as the droplet context's), `threshold`.
- Binaries: `billing` with commands `server`, `worker`, `migrate`, `seed`, `outbox`. The worker
  runs: Period close (first minute of each UTC month), Threshold check (every 15 minutes),
  Dunning (hourly), Exchange rate fetch (daily, with retry until the day has a rate), webhook
  reconciliation (every 5 minutes for Payments still pending), and the event consumers. Jobs
  are plain functions callable once; Redis token locks as in the droplet worker.
- Error domains: 30 profile, 31 paymentmethod, 32 usage, 33 invoice, 34 payment, 35 taxrule,
  36 exchangerate, 37 access.

### Billing profile

- Fields: account type (`individual`, `company`), legal name, country, address lines, city,
  postal code, tax identifiers (TR company: VKN + tax office; TR individual: TCKN; EU: VAT id
  optional; others optional), invoice email, created/updated. One per Team.
- Validation: TCKN 11 digits with the standard checksum, VKN 10 digits; required by country and
  account type; invoice email valid.
- Changing the country when Payment methods exist is refused (409) until they are removed.
- Writes need `billing.write`; reads `billing.read`.

### Payment providers

- One interface, implemented by `craftgate`, `stripe` (built but not routed until an entity
  exists) and `fake`: start card verification (returns what the web app needs to open the
  hosted form), complete card verification (from the provider's callback) → provider customer
  and card references plus brand/last4/expiry; charge a saved card for an amount and currency
  with an idempotency key → result `succeeded`, `failed(reason)`, `confirmation_required(url)`;
  complete a confirmed charge; refund (interface only, not exposed this phase); verify a
  webhook signature and decode it into a provider-neutral event; fetch a payment's current state
  for reconciliation.
- Routing table `country → provider, currency`: `TR → craftgate, TRY`; default → `craftgate,
  USD` (seeded; becomes `stripe, USD` when an entity exists). Stored on the Payment method at
  creation so the provider never changes under a card.
- Craftgate specifics stay inside the adapter: hosted card-verify flow, `cardUserKey/cardToken`
  storage, non-3DS charge first with 3DS init as the fallback that yields
  `confirmation_required`, HMAC webhook verification, payment group for recurring charges.
- Webhook endpoints are public routes on the gateway under the billing target, rate-limited by
  IP; the handler verifies the signature, stores the raw event once (dedupe by provider event
  id), and enqueues reconciliation rather than mutating an Invoice directly.

### Usage

- The droplet context publishes `DropletBillingStarted {droplet_id, team_id, project_id,
  hostname, plan_code, hourly_price, started_at}` when a Droplet first becomes active and
  `DropletBillingStopped {droplet_id, stopped_at}` when deletion is requested or the Droplet
  fails after having been active. The droplet binary gains `replay-billing` to republish for
  existing Droplets.
- Billing keeps one usage row per Droplet with start, stop (nullable) and the Locked price.
  Usage for a Period = ceil(hours of overlap between [start, stop or now) and the Period), capped
  at 672. Pure function, table-tested.
- The draft Invoice's lines are computed on read; nothing is persisted until issue.

### Invoices

- Fields: id, Team, Period start/end, kind (`period`, `threshold`), status (`draft` is virtual;
  persisted statuses `issued`, `paid`, `payment_failed`, `void`), number (`NDR-YYYY-NNNNNN`,
  per-year sequence table), currency `USD`, subtotal, tax total, credit applied, total due,
  settlement currency and amount (TRY equivalent for Turkish Teams), exchange rate and its date,
  a snapshot of the Billing profile at issue, issued-at, paid-at, PDF object key, e-invoice
  reference (nullable, for the issuer adapter), lines (one per Droplet with hours, hourly price,
  amount, droplet id, hostname, plan code), tax lines (rule label, rate, base, amount).
- Issue: in one transaction compute lines from usage for the window, apply Tax rules by the
  profile's country and account type, apply available Credit, allocate the number, snapshot the
  profile and the Exchange rate of the issue date (TR only), persist, then after commit render
  the PDF, publish `InvoiceIssued` (email with PDF link) and create the first Payment. A zero
  total is not issued. A Threshold Invoice covers usage since the last Invoice in the Period.
- PDF: HTML template per Locale rendered through Gotenberg (added to docker-compose and
  dockertest); stored in object storage behind a small interface (local filesystem adapter in
  dev and tests). The PDF carries the "statement" wording until an e-invoice issuer is connected.
- e-invoice issuer interface with `none` adapter: called after issue; a real adapter will set
  the e-invoice reference and may replace the PDF.

### Payments and Dunning

- A Payment row per attempt: Invoice, Payment method, provider, idempotency key
  (`invoice:{id}:attempt:{n}`), amount, currency, status (`pending`, `succeeded`, `failed`,
  `confirmation_required`), provider reference, failure reason, created/updated.
- Charging: the default card; `succeeded` → Invoice `paid`, `PaymentSucceeded` event (receipt
  email); `failed` → Invoice `payment_failed`, Dunning starts; `confirmation_required` → Invoice
  stays `issued`, `PaymentConfirmationRequired` event (email with the confirmation link to the
  web app, which completes it through the adapter).
- Dunning per Invoice from the first failure: retry at +1, +3, +7 days with an email each; on
  the day-7 failure the Team's Billing status becomes `suspended` (event) and the droplet context
  powers off every Droplet of the Team; warnings at day 14 and 20; at day 21 billing publishes
  `TeamDropletsDeletionRequested` and the droplet context deletes the Team's Droplets. A
  successful Payment at any point clears Dunning, sets `active`, and the droplet context powers
  the Droplets back on.
- "Pay now" (web): creates a new Payment attempt on demand (rate-limited 5 per hour per Team),
  optionally with another saved card.

### Billing status and the droplet context

- Billing publishes `TeamBillingStatusChanged {team_id, status, changed_at}` on first Payment
  method added (`active`), on last removed (`no_payment_method`), on suspension and recovery.
- The droplet context keeps the status per Team in its read model (default
  `no_payment_method`), refuses create with 402 "payment method required" (droplet domain) and
  create/power-on with 423 "team suspended", and acts on `suspended` (power off all),
  `active` after suspension (power on all) and deletion requests. Dev seed marks the seed Team
  `active` so local development is not blocked.

### Tax rules and Exchange rates

- Tax rule table: country (or `*`), account type (or `*`), rate (basis points), label key per
  Locale, valid-from. Lookup picks the most specific rule valid at issue. Seed: `TR,* → 2000,
  "KDV %20"`; `*,* → 0, "export-service exemption"`. No operator UI yet; rows by seed/SQL.
- Exchange rates: daily USD buying rate from the Turkish central bank's published XML, stored
  by date; issue uses the rate of the issue date, falling back to the latest available (logged)
  when the day has none yet (weekends, holidays).

### API (billing service, under the gateway target `billing`)

- `GET/PUT /v1/teams/:teamID/billing/profile` [billing.read / billing.write]
- `GET /v1/teams/:teamID/billing/payment-methods` [billing.read];
  `POST .../payment-methods/verify` [billing.write] → hosted form data;
  `POST .../payment-methods/verify/complete` [billing.write] → PaymentMethod;
  `PUT .../payment-methods/:id/default`, `DELETE .../payment-methods/:id` [billing.write]
- `GET /v1/teams/:teamID/billing/usage` [billing.read] → current Period lines and estimate
- `GET /v1/teams/:teamID/billing/invoices` and `/:invoiceID` [billing.read];
  `GET .../invoices/:invoiceID/pdf` [billing.read] → signed download;
  `POST .../invoices/:invoiceID/pay` [billing.write] {payment_method_id?} → Payment
- `GET /v1/teams/:teamID/billing/status` [team.read]: every member sees whether the Team can
  create Droplets; it carries no amounts
- `GET /v1/billing/payments/:paymentID/confirm?token=` public → completes a
  `confirmation_required` Payment after the hosted 3-D Secure step
- `POST /v1/billing/webhooks/craftgate`, `POST /v1/billing/webhooks/stripe` public
- Gateway: third upstream `billing` in all four YAMLs with the permissions above.

### Web app

- Billing area `/dashboard/team/billing` with tabs Profile, Payment methods (hosted card form
  in a modal/iframe, default/delete), Invoices (table, PDF download, Pay now), Usage (current
  Period per Droplet with estimate and the Threshold note).
- Dashboard banner for `no_payment_method` (owner/finance: "Add a card"; others: "Ask an
  owner"), `payment_failed` and `suspended` with Pay now.
- Droplet creation shows the 402/423 reasons with the same calls to action.
- Payment confirmation page `/billing/confirm?payment=&token=` completing the 3-D Secure flow.
- All strings in both locales; money shown exactly as the service formats it.

### Events and emails

- Billing → notification: `InvoiceIssued` (PDF link), `PaymentSucceeded`, `PaymentFailed`
  (with retry day), `PaymentConfirmationRequired`, `TeamSuspended`, `DropletsDeletionWarning`
  (day 14, 20), `CardExpiring`. Recipients: the profile's invoice email plus every owner (from
  the access read model), each in their Locale.

### Decisions made during implementation review

- **3-D Secure starts lazily.** A soft bank refusal marks the attempt `confirmation_required`
  without starting 3-D Secure; the emailed link opens the web app, which asks the billing
  service to start it (`POST /v1/billing/payments/:paymentID/3ds/start?token=`) and shows the
  bank's page. The bank posts back to `POST /v1/billing/payments/:paymentID/3ds/callback?token=`,
  which checks both Nodera's token and the provider's hash, completes the charge and redirects
  to the web app with the outcome. Bank sessions last minutes; the link lasts seven days.
- **Header-less public routes.** Provider webhooks and the 3-D Secure callback cannot send the
  gateway's target header, so the route table marks them `headerless`; the gateway resolves the
  target from the path when exactly one such route matches.
- **Emailed links open the web app.** The Invoice PDF link is
  `{front}/billing/invoice-pdf?invoice=&expires=&token=`; the page downloads through the signed
  public route. The pay link carries the Team (`?team=`).
- **No double charge.** Craftgate does not deduplicate on the attempt key, so a pending attempt
  is looked up at the provider by that key before any new charge, only after twice the provider
  timeout; a Team can have one live attempt per Invoice (409 otherwise).
- **Nothing left uncollected.** A sweep charges issued Invoices without a live attempt and
  fails confirmation links that lapsed, which starts Dunning; Dunning suspends on day 7 whatever
  the retry answered, and deletion is requested only for a suspended Team.
- **Billing cutover.** Usage before `[billing] start_at` is never billed, so replaying past
  Droplets does not invoice hours that were free.
- **Exchange rate.** The latest central-bank bulletin dated strictly before the issue date (to
  be confirmed with the accountant).
- **`replay-status`** republishes every Team's Billing status for the droplet read model.

## Testing Decisions

A good test drives a public interface the way a client would and asserts what the client sees
or what another context would receive: HTTP status and body, an outbox document, a fake
provider's recorded calls, a stored PDF's existence. It never inspects handlers or repositories.

Seams, highest first:

1. **Billing integration suite** (new, primary; mirrors the droplet suite): dockertest
   PostgreSQL, Redis, Mongo and Gotenberg; the billing HTTP server with the fake Payment
   provider; worker jobs and consumers called directly with a controllable clock. Scenarios:
   profile validation by country/account type; card verify start/complete, default, delete rules
   (last card with unpaid Invoice refused); usage from events with rounding, cap, start/stop
   edges across a Period boundary; Threshold Invoice for a new Team at $50 and not at $49;
   Period close issues one Invoice per Team with usage, skips zero totals, numbers sequentially,
   applies TR tax and TRY equivalent with the stored rate, applies Credit first; charge
   succeeded → paid + receipt event; failed → Dunning schedule day by day with emails,
   suspension event on day 7, warnings on 14/20, deletion request on 21; confirmation_required
   → event with link → confirm endpoint completes; Pay now with another card; webhook with bad
   signature rejected, duplicate ignored, valid one reconciled through the fake's state; PDF
   rendered and downloadable; Billing status events on first card / last card removed; access
   read model from customer events authorizes `billing.*` (finance yes, developer 403, outsider
   404).
2. **Droplet integration suite** (existing): `DropletBillingStarted/Stopped` published at the
   right transitions with the Locked price; create refused 402 without Payment method and 423
   when suspended; `suspended` powers off all, recovery powers on, deletion request deletes.
3. **Gateway integration suite** (existing): billing routes with permissions, webhook routes
   public, third upstream.
4. **Notification unit tests** (existing pattern): each new email kind renders in tr and en.
5. **Provider contract test**: fake always; Craftgate sandbox read-only when the smoke env is set
   (webhook signature verification against a recorded sample is part of it).

Unit tests for pure logic: usage hours/cap/rounding, tax rule selection, invoice totals and
rounding (table-driven), invoice numbering, TCKN/VKN checksums, Dunning schedule dates.

Prior art: `cmd/droplet/integration-test` (fake provider, jobs called directly, clock),
`cmd/customer/integration-test` (outbox assertions), gateway suite, notification tests.

## Out of Scope

- Selling Credit (top-ups) and refunds through the UI; both are modelled, neither exposed.
- Stripe routing in production (adapter is built and contract-tested; routing waits for a
  non-Turkish entity), Stripe Billing/Tax products.
- A real e-invoice integrator (interface and `none` adapter only), legal e-Arşiv issuance.
- EU VAT OSS rules, per-Project cost allocation, budgets and spending alerts beyond the
  Threshold, cost export (CSV).
- Installments, bank transfer, wallets, pay-by-link.
- Operator UI for Tax rules, Thresholds and routing (seed/SQL only).
- Changing the Period to anything but the UTC calendar month.
- Disputes/chargebacks handling.

## Further Notes

- Operational prerequisites on Nodera's side: Craftgate account and sandbox keys; TRY and USD
  virtual POS contracts with a Turkish bank, one enabled for merchant-initiated charges without
  3-D Secure; accountant confirmation of the 20% KDV rule, the export-service exemption, the
  central-bank buying-rate rule and the e-Arşiv integrator choice.
- Until an e-invoice issuer is connected, the PDF says "statement"; the model already carries
  every field the integrator will ask for (ADR-0006).
- The exchange difference between the issue-date rate and a later collection stays with
  Nodera and never appears on the customer's statement.
- Provider names never reach a DTO, an error message or the web app, consistent with the
  droplet context; the hosted card form is the one place a customer meets the Payment provider,
  by necessity.
