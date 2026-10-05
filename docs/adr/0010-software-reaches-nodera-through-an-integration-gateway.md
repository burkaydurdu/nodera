---
status: accepted
---

# Software reaches Nodera through an integration gateway, with API keys that belong to a Project

A Team's software calls Nodera from its own servers, not from the web app: Mail's sending API now,
Droplets later. The customer gateway cannot take that traffic. It knows only a User's access token,
accepts 2 MB bodies, limits each IP to 50 requests a second and is shaped around the web app (CORS,
the Team in the path). So a second gateway, the integration gateway, fronts the services for
software: the same deny-by-default route table split per upstream (`mail` now, `droplet` later),
authenticated by an API key and limited per key. SMTP is not HTTP, so the mail service's SMTP
listener checks the same API key itself.

An API key belongs to a Project, acts only on that Project and carries Permissions from the same
`pkg/authz` vocabulary (`mail.send` first), never wider than its creator's. The customer context
keeps API keys, beside Users, Teams and Sessions. We weighed keys owned by each product, which
would make a customer hold one key per product instead of one place to come with a key, and keys
owned by a User, like personal tokens, which would stop a Team's production sending the day the
User who made the key leaves.

## Consequences

- The services enforce a key's Permission themselves, as they do a Role's (ADR-0005): the gateway
  is the first gate, never the only one.
- Like a Session, a key reaches the gateway and the services through Redis: the customer context
  writes it there on creation and deletes it on revocation, and the gateway and the mail service read
  it, so a revocation is effective on the next request without any event. The customer context can
  rewrite every key from its database if Redis is ever lost.
- Removing a member leaves the keys they created working; a Team revokes keys itself.
- A route software may call exists only in the integration gateway's own route table, one file per
  environment like the customer gateway's.
