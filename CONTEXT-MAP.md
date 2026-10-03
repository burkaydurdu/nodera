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
- [Notification](./contexts/notification/CONTEXT.md): what the platform tells people about
  what happened, recorded and delivered over Email, SMS and In-app Channels.
- [Droplet](./contexts/droplet/CONTEXT.md): the catalog (Regions, Plans, Offers, Commission),
  the Providers behind it and the Droplets rented through them.
- [Billing](./contexts/billing/CONTEXT.md): what a Team owes for its Droplet hours, how it pays and
  what it is told; Payment providers behind it (ADR-0006).
- Realtime (`contexts/realtime/`): platform infrastructure, like the gateway, not a bounded
  context: the web app's live socket, which knows Users, Teams and Permissions from the access
  token and nothing else (ADR-0007).
- Admin (planned): the operator-facing side. Not started.

## Relationships

- **Customer gateway → Customer**: routing plus per-endpoint access rules. A route absent from the
  gateway's config does not exist. For routes that name a Team, the gateway also checks the
  caller's Role from the access token against the route's Permissions (ADR-0005).
- **Customer → Notification**: customer emits email events (verification code, MFA code, password
  reset, invitation); notification records and sends them, never keeping the code or link.
  Notification also keeps a Team read model from the Team and profile events, decides from it who
  is told about a Team's things (ADR-0008), and tells a member of a role change or removal someone
  else made.
- **Droplet → Notification**: droplet emits every change of a Droplet's status; notification
  makes Notifications of the ones worth telling (ready, failed).
- **Customer → Droplet**: customer emits Team and Project events (team created, member added,
  project created); droplet keeps a Project access read model from them (ADR-0004).
- **Droplet → Customer**: a Droplet references a Project by identifier only; the droplet context
  never reads Teams or Users.
- **Droplet → Billing**: droplet emits usage events (billing started/stopped per Droplet with the
  Locked price); billing meters them.
- **Billing → Droplet**: billing emits a Team's Billing status; droplet refuses creation without
  a Payment method and stops Droplets of a suspended Team.
- **Customer → Billing**: billing learns Teams and their members from the customer events, like
  droplet does (ADR-0004), to authorize `billing.*`.
- **Billing → Notification**: what happened to a Team's money (Invoice issued, Payment outcome,
  suspension, deletion warnings, an expiring card), naming the Team and the billing profile's
  invoice address; notification tells the members holding `billing.read` and that address.
- **Customer gateway → Billing**: the gateway fronts the billing service as a third target.
- **Customer gateway → Droplet**: the same gateway fronts the droplet service under a second
  target; a droplet route absent from its config does not exist.
- **Customer gateway → Notification**: the gateway fronts the Inbox as a fourth target.
- **Contexts → Realtime**: any context publishes a realtime message (an address, a type, data)
  through its outbox; the realtime service hands it to the open sockets the address names. It
  carries no domain event and never goes from one context to another (ADR-0007).
- **Customer gateway → Realtime**: the socket's route is headerless and public; the realtime
  service authenticates the token in the first frame.
