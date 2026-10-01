---
status: accepted
---

# Team Roles ride the access token, with a permission version for freshness

Permissions in Nodera belong to a Role in a Team, and most protected routes name their Team
in the path. The customer gateway is the first gate for every request, so it should refuse a
request the caller's Role cannot make before it reaches a service. The gateway only has the
session and the JWT, and it must not call the customer service or read its tables on every
request. The access token therefore carries the caller's Role per Team (`teams`: Team id →
Role) and the gateway maps Role to Permissions through the shared, static matrix in
`pkg/authz` that the services use as well. Services still enforce the same check themselves:
the gateway is a first gate, never the only one, and routes without a Team in their path (a
Droplet by id, the profile) are checked by their service alone.

A Role change, a removal or an accepted Invitation must take effect at once, not when a
15-minute token expires, and must not throw the User out of every device. So each User has a
permission version, kept in Redis and stamped into the token as `pv`. The customer service
bumps it on every membership change; the gateway compares the token's `pv` with Redis on each
request and answers 401 to a stale token. The web app's existing 401 → refresh path then mints
a new access token whose claims are read from the database, and the User continues.

## Consequences

- Access tokens issued before this decision carry no `teams`/`pv` claim and are treated as
  stale: one silent refresh per device after deployment, no sign-in.
- Issuing an access token now reads the User's memberships; refresh does the same, so a
  refresh is where new Roles arrive.
- The gateway reads two Redis keys per authenticated request instead of one. Both are cheap
  point reads and can be pipelined.
- A User in a very large number of Teams would grow the token. Teams are small-count per User
  in this product; if that changes, the claim becomes a reference and the gateway reads the
  Role map from Redis instead.
- The matrix is code. A Team cannot define its own Roles, by design.
