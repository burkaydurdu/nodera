---
status: accepted
---

# The droplet context learns Projects and their members from events

A Droplet belongs to a Project, and only a member of that Project's Team may see or act on it.
Projects, Teams and members live in the customer context, in its own database; the droplet
context has its own. Rather than let the droplet service query the customer service (an HTTP
call on every request, and a hidden dependency at start-up) or read its tables (one schema
shared by two contexts), the customer context publishes `team.created`, `team.member_added` and
`project.created` events through its outbox and the droplet context keeps a small read model,
Project access, fed by a consumer of those events. Authorization inside the droplet service is a
local lookup.

## Consequences

- Project access is eventually consistent. A Project created a moment ago may not be known to
  the droplet service yet; the droplet API answers `project not found` and the web app retries
  briefly after creating a Project before it offers to create a Droplet.
- The customer context's outbox now carries domain events as well as emails; the notification
  worker ignores the ones it does not consume.
- Existing rows created before the events existed must be replayed once; the customer binary
  gets a `replay-projects` command for that.
- Removing a member or deleting a Project, when those arrive, are new events and a read-model
  update, not a new integration.
