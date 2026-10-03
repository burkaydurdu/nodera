---
status: accepted
---

# The notification context decides who is told

A Notification about a Team goes to the members whose Role holds the Permission to see what it is
about (`pkg/authz`): a Droplet's to `droplet.read`, an Invoice's to `billing.read`. The
notification context keeps its own Team membership read model from the customer events, as droplet
and billing do (ADR-0004), and resolves Recipients itself. Producing contexts publish what
happened, with the Team's identifier, and never a recipient list. We weighed letting each producer
resolve Recipients from its own read model, which billing already did, but that spreads the rule
"who is told" across every context and lets it drift from who may see the thing.

## Consequences

- Billing's events carry no recipient list: they name the Team, the Invoice, Payment or card, and
  the billing profile's invoice address, which this context tells by email as an extra Recipient
  unless a member already holds it. Finance members, who hold `billing.read`, now receive billing
  emails as well as owners.
- Billing's Team read model no longer keeps contact details: it only authorizes.
- The customer context publishes a profile change (name, locale) so this read model writes to
  people in their current name and language; a membership event older than the change cannot
  write the old ones back.
- Membership changes name who made them, so a member is told of a role change or a removal only
  when someone else made it.
- Membership is eventually consistent here too: someone added to a Team a moment before a Droplet
  becomes active may miss that one Notification.
