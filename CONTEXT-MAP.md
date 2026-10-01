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
- Admin (planned): the operator-facing side. Not started.

## Relationships

- **Customer gateway → Customer**: routing plus per-endpoint access rules. A route absent from the
  gateway's config does not exist.
- **Customer → Notification**: customer emits email events (verification code, MFA code, password
  reset); notification consumes them and sends the mail.
- **Customer → Droplet**: customer emits Team and Project events (team created, member added,
  project created); droplet keeps a Project access read model from them (ADR-0004).
- **Droplet → Customer**: a Droplet references a Project by identifier only; the droplet context
  never reads Teams or Users.
- **Customer gateway → Droplet**: the same gateway fronts the droplet service under a second
  target; a droplet route absent from its config does not exist.
