# Context Map

Nodera is a self-service cloud platform: a customer asks for a CPU/RAM/disk/region combination and
receives a Droplet, which is a bare-metal server the platform rents from an integrated provider and
sets up on the customer's behalf. The customer never sees the provider.

Bounded contexts live under `contexts/<name>/`. A context gets a `CONTEXT.md` when its vocabulary
is first written down, so this list grows as terms are settled.

## Contexts

- [Customer](./contexts/customer/CONTEXT.md): the customer-facing side — the people who sign up,
  how they prove who they are, and the teams and projects their Droplets will be grouped under.
- Customer gateway (`contexts/gateway/`): the reverse proxy in front of the customer service and
  the authorization source of truth for its routes.
- Notification (`contexts/notification/`): the emails the platform sends.
- [Droplet](./contexts/droplet/CONTEXT.md): the catalog (Regions, Plans, Offers, Commission),
  the Providers behind it and the Droplets rented through them.
- [Billing](./contexts/billing/CONTEXT.md): what a Team owes for its Droplet hours, how it pays and
  what it is told; Payment providers behind it (ADR-0006).
- Admin (planned): the operator-facing side. Not started.

## Relationships

- **Customer gateway → Customer**: routing plus per-endpoint access rules. A route absent from the
  gateway's config does not exist. For routes that name a Team, the gateway also checks the
  caller's Role from the access token against the route's Permissions (ADR-0005).
- **Customer → Notification**: customer emits email events (verification code, MFA code, password
  reset); notification consumes them and sends the mail.
- **Customer → Droplet**: customer emits Team and Project events (team created, member added,
  project created); droplet keeps a Project access read model from them (ADR-0004).
- **Droplet → Customer**: a Droplet references a Project by identifier only; the droplet context
  never reads Teams or Users.
- **Droplet → Billing**: droplet emits usage events (billing started/stopped per Droplet with the
  Locked price); billing meters them.
- **Billing → Droplet**: billing emits a Team's Billing status; droplet refuses creation without
  a Payment method and stops Droplets of a suspended Team.
- **Customer → Billing**: billing learns Teams and their members from the customer events, like
  droplet does (ADR-0004), to authorize `billing.*` and to address invoice emails to owners.
- **Billing → Notification**: invoice, payment and dunning emails.
- **Customer gateway → Billing**: the gateway fronts the billing service as a third target.
- **Customer gateway → Droplet**: the same gateway fronts the droplet service under a second
  target; a droplet route absent from its config does not exist.
