---
status: accepted
---

# Mail delivers from Nodera's own MTA, not through a resold sending service

Droplets resell a Provider's servers (ADR-0003), and Mail could have resold a sending service the
same way. We researched that on 2026-10-04: Amazon SES tenants isolate each customer on any plan at
about $0.10 per 1,000 emails, and Elastic Email's terms allow resale through sub-accounts, while
SendGrid caps subusers at 15, Mailgun keeps sub-accounts for Enterprise, and the resale clauses of
SES ("resell the Services"), SendGrid ("standalone basis") and Postmark would each need written
consent. We chose to deliver from Nodera's own MTA instead: Mail is meant to be a sending platform
Nodera runs itself, as SendGrid and Mailgun run theirs, not a margin on someone else's.

The engine is KumoMTA (Apache-2.0). It brings the queues, per-destination traffic shaping, retries,
bounce classification, DKIM signing and IP pools that take years to get right, and it has no
management API: the mail service is the source of truth, and KumoMTA's Lua policy reads Sending
domains and DKIM keys from it. We rejected writing an MTA in Go (all of that rebuilt), Postfix and
Haraka (no multi-tenant control to build on), and Postal, whose API cannot manage organisations,
domains or credentials, so its settings could not live in Nodera's web app.

The MTA servers and their IPs run on Cherry Servers, and only once Cherry confirms in writing that
port 25 is open to them, that running a sending service for third parties is allowed, and that a
clean IP block (a /28 or larger) is available. Cherry documents none of this. It does document that
it may block SMTP at once after a spam incident, and that it charges €100 plus €100 per hour for a
blocklisting caused by "You or Your End Users".

## Consequences

- Delivery reputation is Nodera's. Every Team sends from the same IPs, so one Team's spam harms
  every other Team and can cost Nodera Cherry's SMTP block and fees. The abuse rules are part of
  the product: mail leaves only from a verified Sending domain, a new Project starts at a low Daily
  limit that rises while its mail is clean, Suppressions are kept per Project, and a Project whose
  bounce or complaint rate crosses the line is Paused.
- Until the Admin side exists, lifting a Pause or changing a Package's price is operator work in
  the database.
- Running Mail means running a sending platform: PTR records on every IP, feedback-loop
  registrations, postmaster tools, abuse@ and postmaster@ mailboxes, blocklist monitoring and a
  warm-up for every new IP.
- There is no Mail provider and no adapter interface for one. If Nodera ever resells a sending
  service, it arrives as a second delivery path then.
- Dedicated IPs are a later add-on; at launch every Team sends from the shared pool.
- Nodera's own email (the notification context's) stays on SendGrid until Mail has a separate
  system IP pool, so that no Team's reputation can ever block a verification code or a password
  reset.
