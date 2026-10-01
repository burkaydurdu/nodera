---
status: accepted
---

# MongoDB is in the stack only to back the outbox

Domain data lives in PostgreSQL, but cross-service events go out through the `piggystats/outbox`
library, which stores its messages in MongoDB and nothing else. We chose to add a MongoDB replica
set for that one collection over writing a PostgreSQL outbox of our own, because the library and
its relay are already proven in the sibling Apex repos and the platform's engineers know its
failure modes. The known cost, accepted knowingly: the outbox insert is not part of the PostgreSQL
transaction it follows, so a crash between the commit and the insert loses that event.

## Considered options

- **PostgreSQL outbox in `pkg/`, same `PublishWithOutbox` API.** Transactional with the domain
  write; rejected for now as new code to own and test before the first feature ships. It stays the
  natural replacement if the lost-event window ever bites.
- **No outbox, publish straight to RabbitMQ.** Rejected: a broker outage silently drops emails.

## Consequences

- MongoDB (single-node replica set, transactions need one) is part of docker-compose and of the
  dockertest harness. No other code may write to it.
- A handler that publishes must do so after `Commit`, never inside the transaction, so a rollback
  cannot leave a published event behind.
