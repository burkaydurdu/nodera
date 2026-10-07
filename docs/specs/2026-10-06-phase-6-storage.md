# Phase 6: Storage — Storages on Packages, Access keys, the file browser and the Pause

Vocabulary: `contexts/storage/CONTEXT.md` (Storage, Bucket name, Region, Access key, Package, Pause,
Billing status), `contexts/customer/CONTEXT.md` (App, API key, Project, Role, Permission),
`contexts/billing/CONTEXT.md` (Period, Package, Invoice, Invoice line, Threshold, Billing status,
Dunning). Decisions already recorded: ADR-0001 (outbox), ADR-0004 (read models from events), ADR-0005
(Roles in the token), ADR-0007 (live updates), ADR-0008 (who is told), ADR-0010 (API keys, which
Storage does not use), ADR-0012 (Storage keeps objects on Nodera's own Ceph cluster, one bucket per
Storage).

## Problem Statement

A Team that runs software on Nodera has files to keep: uploads from its users, images its site
serves, build artefacts, backups. A Droplet's disk is the wrong place for them, so today the Team
opens an account with an object store such as DigitalOcean Spaces or Amazon S3, and keeps a second
dashboard, a second card and a second bill. Nodera wants to be that object store itself: a third
product, Storage, kept on Nodera's own storage servers, spoken to with the S3 tools the Team already
uses, configured inside the Nodera web app as an App of its own, and charged on the same monthly
Invoice as the Team's Droplets and Mail.

## Solution

Inside a Project, a Team opens the Storage App and opens a Storage: it gives a Bucket name, picks a
Region and a Package (a size with a monthly price), and receives the address of an S3 bucket. It
makes an Access key for that Storage, to read it or to read and write it, and its software then
talks S3 straight to Nodera's storage servers: uploads, downloads, multipart, versioning, lifecycle
rules, bucket policies and ACLs, publishing objects included. Only opening and deleting a bucket
stay with Nodera, because each bucket is a purchase.

The Team sees its Storages with how full each is, browses, uploads, downloads and deletes files in
the web app, and is told when a Storage is nearly full, full or Paused. A full Storage takes no more
writes until it moves to a larger Package or files are deleted. Each Storage's Package lands on the
Team's Invoice: the largest Package a Storage held in a Period is what it pays for that Period. An
unpaid Invoice stops the Team's Storages and, on day 21, deletes what they hold; a complaint about
what a Storage publishes lets an operator Pause it.

This phase delivers the software against a fake engine and a single-container Ceph. The production
cluster is ordered and installed afterwards (ADR-0012).

## User Stories

### The Storage App

1. As a User, I want the App switcher to offer Storage beside Droplets and Mail, so that I can move
   to my files like to any other product.
2. As a User, I want the side menu to show only Storage's pages while I am in it, so that it never
   crowds the other Apps.
3. As a User, I want the Storage App to follow the Project selector in the top bar, so that switching
   App keeps me in the same Project.
4. As a member without `storage.read`, I want the Storage App to say my Role cannot see Storage, so
   that I know why it is empty.
5. As a member with `storage.read`, I want a list of the Project's Storages with Bucket name, Region,
   Package, how full each is and its state, so that I see everything at a glance.

### Opening a Storage

6. As a developer, I want to open a Storage by giving a Bucket name and choosing a Region and a
   Package, so that I get a bucket in one step.
7. As a developer, I want to see the Packages with their sizes and monthly prices before I choose, so
   that I know what I will pay.
8. As a developer, I want to be told the rules for a Bucket name while I type (3 to 63 characters,
   lower-case letters, digits and hyphens), so that I do not submit a name that is refused.
9. As a developer, I want to be told when a Bucket name is already taken in that Region, so that I
   can pick another.
10. As a developer, I want names that look like Nodera's own refused, so that nobody can pose as
    Nodera from a Storage's address.
11. As an owner of a Team without a payment method, I want to be told to add a card before opening a
    Storage, so that I know what blocks me.
12. As a member of a suspended Team, I want opening a Storage refused with the reason, so that I
    know the unpaid Invoice is what blocks me.
13. As a developer, I want the Storage usable the moment the request returns, so that I can point my
    software at it at once.
14. As a developer, I want to see a Storage's S3 endpoint, Region and Bucket name with copy buttons,
    so that I can configure any S3 tool.
15. As a developer, I want a Storage's Bucket name and Region never to change, so that addresses my
    software stores stay valid.
16. As a developer, I want several Storages in one Project, each with its own Package, so that I can
    keep unrelated files apart.

### Access keys

17. As a developer, I want to make an Access key for a Storage with a name I choose, so that I can
    tell my keys apart.
18. As a developer, I want to choose whether an Access key may only read or may read and write, so
    that software gets no more than it needs.
19. As a developer, I want to see the secret key once, when the Access key is made, so that it never
    sits in Nodera for someone else to read.
20. As a developer, I want to list a Storage's Access keys with their names, access key IDs, what
    they may do, who made them and when, so that I can audit them.
21. As a developer, I want to revoke an Access key and have it refused on its next request, so that
    a leaked key stops working at once.
22. As a Team, I want an Access key to keep working after the member who made it leaves, so that
    production does not break when people change.
23. As a developer, I want an Access key to reach only the Storage it was made for, so that a leak
    never exposes another Storage.
24. As software, I want a clear S3 error when my key is revoked, read-only or made for another
    Storage, so that I can tell a wrong key from a missing object.

### Using a Storage over S3

25. As software, I want to upload, download, list and delete objects with any S3 SDK or tool, so
    that I change nothing but the endpoint and the key.
26. As software, I want multipart uploads, so that large files go up in parts.
27. As software, I want presigned URLs, so that my users can download or upload without my keys.
28. As a developer, I want versioning, lifecycle rules, CORS, bucket policies and ACLs on my Storage,
    so that it behaves like the S3 bucket my software expects.
29. As a developer, I want to publish an object, or everything under a prefix, to anyone without a
    key, so that my site can serve its images from Storage.
30. As a developer, I want objects to stay private unless I publish them, so that nothing leaks by
    default.
31. As software, I want creating or deleting a bucket over S3 refused with a clear error, so that I
    learn buckets are opened in Nodera.
32. As software, I want to reach my Storage at `<bucket name>.<region>.<domain>` and by path, so
    that both S3 addressing styles work.

### The file browser

33. As a member with `storage.read`, I want to browse a Storage's files by folder in the web app, so
    that I can check what is there without a tool.
34. As a member with `storage.read`, I want to download a file from the browser, so that I can look
    at it.
35. As a developer, I want to upload files from the browser, so that I can put a few files in
    without writing code.
36. As a developer, I want to delete selected files from the browser, so that I can free room.
37. As a developer, I want to be told when my own CORS rules stop the browser's uploads and to
    restore Nodera's rule with one click, so that I am never stuck.
38. As a member, I want long folders paged, so that a Storage with a million objects still opens.

### Packages and being full

39. As a member with `storage.read`, I want to see how much of its Package each Storage holds and
    how many objects, so that I can plan.
40. As a developer, I want to move a Storage to a larger Package at once, so that writes resume
    immediately when it is full.
41. As a developer, I want a move to a smaller Package to take effect at the start of the next
    Period, so that I keep what this Period already costs me.
42. As a developer, I want a move to a smaller Package refused while the Storage holds more than
    that Package, so that I never end up over my limit by choice.
43. As a developer, I want to undo a scheduled move before the Period ends, so that a mistake costs
    nothing.
44. As a member with `storage.read`, I want to be told when a scheduled move was dropped because the
    Storage no longer fitted, so that I am not surprised by the Invoice.
45. As software, I want writes refused with a clear "quota exceeded" S3 error when the Storage is
    full, so that I can surface it.
46. As software, I want reads and deletes to keep working when the Storage is full, so that my site
    stays up and I can make room.
47. As a member with `storage.read`, I want to be told at 80% and when the Storage is full, so that
    I can act before, or right after, writes stop.
48. As a Team, I want to be told again if a Storage empties and fills up a second time, so that one
    warning long ago is not the only one.
49. As a Team, I want the little a Storage may pass its Package by before Nodera notices not to be
    charged, so that the price I chose is the price I pay.
50. As a member with `storage.read`, I want to see each Storage's size and its traffic in and out
    per day, so that I understand how it is used.

### Deleting a Storage

51. As a developer, I want to delete a Storage and everything in it by typing its Bucket name, so
    that I cannot destroy data by a slip.
52. As a developer, I want a deleted Storage's Access keys to stop working at once, so that nothing
    writes into a Storage that is going away.
53. As a developer, I want the Storage's Package to end the moment I delete it, so that no later
    Period charges it.
54. As a developer, I want a deleted Storage's Bucket name to become free once its files are gone,
    so that I can open it again.
55. As a member, I want to see a Storage as "deleting" while Nodera removes its files, so that I know
    it is not stuck.

### Billing

56. As a finance member, I want one Invoice line per Storage for the Period, showing its Bucket
    name, the Package and the amount, so that I can reconcile it.
57. As a finance member, I want the largest Package a Storage held in a Period to be what it pays,
    without proration, whether it was opened on the 25th or deleted on the 3rd, so that the rule
    fits in one sentence.
58. As a finance member, I want a Storage already charged on a Threshold Invoice not charged again,
    only the difference after a move to a larger Package, so that nothing is billed twice.
59. As a finance member, I want Storage lines taxed like Droplet and Mail lines, so that the totals
    are consistent.
60. As a finance member, I want to see each Storage's Package in the web app, so that I understand
    the Storage lines on the Invoice.
61. As a Team, I want traffic not to appear on the Invoice, so that the Package's price is all I pay.

### Unpaid Invoices

62. As a suspended Team, I want my Storages to stop being read and written while my files stay, so
    that paying brings everything back.
63. As a Team that paid, I want my Storages to work again without doing anything, so that recovery
    is automatic.
64. As a Team on its way to day 21 of Dunning, I want the warnings to say that what my Storages hold
    will be deleted with my Droplets, so that I am not surprised.
65. As a Team reaching day 21 of Dunning, I want my Storages deleted and their Packages ended, so
    that I owe nothing for later Periods.
66. As a Team whose card was removed, I want my existing Storages to keep working until an Invoice
    goes unpaid, so that removing a card is not an outage.

### Roles and Permissions

67. As an owner, I want owners and developers to manage Storage and finance members to only see it,
    so that Roles match responsibilities.
68. As a member without the needed Permission I want 403, and as a stranger 404, so that Storage
    behaves exactly like Droplets and Mail.
69. As a finance member, I want the file browser to show me files but refuse uploads and deletes, so
    that seeing does not mean changing.

### Abuse and the Pause

70. As an operator, I want to Pause a Storage with one command and a reason, so that content someone
    complained about stops being served within minutes.
71. As an operator, I want a Paused Storage to refuse every read and write, published objects
    included, while its files stay, so that I can reopen it after talking to its Team.
72. As an operator, I want to lift a Pause with one command, so that reopening is as quick as
    stopping.
73. As a member with `storage.read`, I want to be told that a Storage was Paused and why, so that I
    can fix it or write back.
74. As a member, I want a Paused Storage shown as Paused with the reason in the web app, so that I do
    not mistake it for an outage.
75. As a developer, I want to be able to delete a Paused Storage, so that I can remove what was
    complained about myself.
76. As a Team, I want a Storage that is both Paused and suspended to come back only when neither
    holds, so that paying does not undo a Pause.
77. As Nodera, I want Storage served from a domain of its own, so that a complaint about a
    customer's file never touches the web app's domain.

### Operating Nodera

78. As an operator, I want the Package catalog in a table seeded at launch, so that the Admin side
    can edit it later.
79. As an operator, I want the Regions in a table and the engine's endpoints and keys in
    configuration, so that environments differ only by config.
80. As an operator, I want each Storage rate limited from configuration, so that one Storage cannot
    use up the cluster's port.
81. As an operator, I want every Storage's traffic recorded from the first day, so that I can see
    who uses the network before transfer is ever sold.
82. As an operator, I want whatever Nodera asked of the engine and did not get retried until it
    holds, so that a Ceph restart never leaves a Storage half open, half stopped or half deleted.
83. As an operator, I want an opt-in contract test against a real Ceph, so that the adapter, the
    quota, the key policies and the Pause are checked together.
84. As an operator, I want a Ceph in docker-compose for development, so that the whole flow runs on a
    laptop.

## Implementation Decisions

### The storage context

- A new bounded context, `storage`, with its own PostgreSQL database, its own Mongo outbox and its
  own consumers, layered like the mail context: `api/v1` binds and validates, the application
  handlers hold the rules, aggregates own the model, requests, errors, a thin service and the pgx
  repository.
- One binary with the commands `server` (the management API for the web app), `worker` (consumers
  and jobs under Redis locks), `outbox`, `migrate`, `rollback`, `seed`, `seed-rollback` and the
  operator commands `pause` (a Storage and a reason code) and `unpause`.
- Aggregates: `access` (Project access with each Project's name), `billing` (Billing status),
  `catalog` (Regions and Packages), `storage` (Storages, their Package, scheduled change, Pause and
  measured size), `accesskey`, `object` (the file browser's requests and errors; it owns no table),
  `usage` (size and traffic per Storage and day). No aggregate imports a sibling.
- Jobs, each on its own interval and lock: reconcile (bring the engine to what the database says),
  measure (size and object count), traffic (the engine's usage log), Period rollover (scheduled
  Package changes), purge (deleting Storages).

### Project access, Project names and Billing status

- The same event-fed read models as the droplet and mail contexts (ADR-0004): memberships are
  last-writer-wins on the event's own time with tombstones for removals; a Team never heard of has no
  payment method. `ProjectCreated` already carries the Project's name; the customer context changes
  nothing in this phase.
- `TeamBillingStatusChanged` feeds the Billing status. `TeamDropletsDeletionRequested` (day 21 of
  Dunning) deletes every Storage of the Team. The event keeps its name; mail already listens to it
  for the same day.

### The engine port

- The handlers consume one interface, the engine, and know nothing of Ceph. It speaks Nodera's
  language: ensure a Project's account; open a Storage's bucket; set its size limit and its rate
  limit; stop and resume a Storage; delete a Storage with everything in it; make and revoke an
  Access key; read a Storage's size and object count; read its traffic for an hour range; list
  objects, delete objects and presign a download or an upload.
- Two adapters: Ceph (the admin API over HTTP, the IAM API and S3 itself, signed with the Project
  account's root key) and a fake that keeps buckets, keys and sizes in memory, records every call
  and can be told to fail. The fake serves the integration suite; development runs the real adapter
  against a Ceph container.
- In Ceph (ADR-0012): a Project is an account, made the first time the Project opens a Storage; a
  Storage is a bucket of that account whose bucket quota is the Package's size; an Access key is a
  user of the account with one key and one policy naming the Storage's bucket. A read key's policy
  allows getting and listing; a read-and-write key's allows every S3 action on the bucket and its
  objects. No policy allows creating or deleting a bucket.
- The account's root key is what the storage service acts with. It is kept sealed (AES-256-GCM, the
  key from configuration) in the storage database. The sealing code mail already has moves to a
  shared package so that both contexts use it without importing each other.
- "Stop" is one operation for both a Pause and a suspended Team: after it, no key and no anonymous
  request reads or writes the Storage, and its files and its own policies are intact when it is
  resumed. Ceph 20.2.4 has no command for it, so the adapter builds it; the first candidate is
  turning every key of the Storage to deny-all and adding a reserved deny statement to the bucket's
  policy. The contract test proves the mechanism, published objects included, or replaces it; the
  port does not change.

### Regions and Packages

- Regions are a seeded table (code, city, country, active) using the droplet context's codes. The
  launch Region is `vno1`. The engine's endpoints and admin key for that Region are configuration;
  a second Region is out of scope.
- The Package catalog is a seeded table: a code, a size in bytes and a monthly price in USD
  excluding tax, `numeric` in the database and `decimal` in Go. 1 GB is 1,000,000,000 bytes. The
  seed is provisional until Cherry has quoted the cluster: `10gb` for $1, `50gb` for $3, `250gb` for
  $10, `1tb` for $35. It is replaced before launch.
- A Storage's Package copies the catalog's size and price when it is chosen, like a Droplet's Locked
  price; a smaller Package scheduled for the next Period copies its terms when it is scheduled.

### Opening a Storage

- Needs `storage.write` and an `active` Billing status (402 without a payment method, 423 when
  suspended).
- A Bucket name is 3 to 63 characters of lower-case letters, digits and hyphens, starts and ends
  with a letter or a digit, has no dots (the wildcard certificate covers one label) and is not on
  the reserved list (Nodera's own names and common service names). It is unique in its Region among
  all Storages not yet fully deleted, whichever Team they belong to (409 when taken).
- A Project holds at most 100 Storages.
- Opening is synchronous. The name is reserved, the engine ensures the account, opens the bucket,
  sets the size limit, the rate limit and a CORS rule for the web app's origin, and only then is the
  Storage `active` and its Package started. If the engine cannot be reached the request answers 503
  and nothing is opened: the engine is asked to remove whatever it may have made and the name is
  freed; when it cannot even be asked, the reservation stays until the reconcile job removes both. A
  reservation left behind by a crash goes the same way. An engine that answers no is a 500, not
  something to repeat.
- A Storage is `active`, `deleting` or `deleted`. What the web app shows on top of that is derived
  in this order: suspended, paused, full, active.

### Changing the Package

- Needs `storage.write` and an `active` Billing status.
- A larger Package takes effect at once: the size limit is raised, and billing sees a stop and a
  start at the same instant.
- A smaller Package is scheduled for the start of the next Period, only if the Storage's measured
  size fits it (409 otherwise). Choosing the current Package again clears the schedule.
- The rollover job, idempotent and run every 15 minutes, applies a scheduled change at the Period's
  first instant if the Storage still fits; if it no longer does, the schedule is dropped, the
  Storage stays on its Package and `StorageDowngradeSkipped` is published.
- A Storage always has a Package; ending it means deleting the Storage.

### Size, being full and traffic

- The measure job reads each active Storage's size and object count from the engine every few
  minutes and keeps the latest values, with the time they were read, on the Storage.
- Crossing 80% of the Package publishes `StorageNearlyFull`, reaching 100% `StorageFull`. Each mark
  is armed again when the Storage falls back under it or moves to a larger Package, so a second
  filling is told again, and never more than once per crossing.
- The engine refuses writes to a full Storage by itself, with S3's 403 `QuotaExceeded`; it works
  from cached statistics, so a Storage can pass its Package by what arrives in the minutes before
  the engine notices. Nothing is done about the excess and nothing is charged for it.
- The traffic job reads the engine's usage log and keeps, per Storage and hour, the bytes sent,
  the bytes received and the number of requests, shown per UTC day; an hour read again replaces
  what was kept, so a log still being written is corrected, never doubled. Nothing is billed from
  it in this phase.
- Every Storage gets the rate limit from configuration (requests and bytes a minute, in each
  direction) when it is opened.

### Access keys

- An Access key belongs to one Storage, has a name, and may `read` or `read_write`. Making and
  revoking need `storage.write`; listing needs `storage.read`. A Storage holds at most 20.
- Making one is synchronous against the engine and answers the access key ID and the secret key
  once. Nodera keeps the name, the access key ID, what it may do, who made it and when; never the
  secret key.
- Revoking removes the key from the engine before the request returns; 503 if the engine cannot be
  reached, and the key stays listed.
- Removing a member revokes nothing. Deleting a Storage revokes all of its Access keys.

### The file browser

- Listing goes through the storage service, which lists with the account's root key: a prefix, `/`
  as the folder delimiter and a continuation cursor, 200 entries a page. Needs `storage.read`.
- A download is a presigned GET the service hands out for one object, valid for five minutes; the
  browser follows it. Needs `storage.read`.
- An upload is a presigned PUT the service hands out, valid for fifteen minutes; the browser sends
  the file straight to the Storage, never through Nodera's gateway. One file is at most 5 GB; the
  web app points to S3 tools for more. Needs `storage.write`.
- Deleting takes up to 1,000 object keys in one request and goes through the service. Needs
  `storage.write`.
- Browser uploads depend on the CORS rule set at opening. If the Team's own CORS rules replaced it,
  the upload fails in the browser, the web app says so and offers to restore the rule, which adds
  Nodera's rule to the bucket's existing ones.
- A stopped Storage (Paused, or its Team suspended) refuses the browser too, with 423.

### Deleting a Storage

- Needs `storage.write`; the request repeats the Bucket name and is refused if it does not match.
  Allowed whatever the Billing status and whether or not the Storage is Paused.
- At once: the Storage is `deleting`, its Access keys are revoked, its Package is stopped at that
  instant and its scheduled change is dropped. The purge job then removes every object and version,
  then the bucket, and marks the Storage `deleted`, which frees its Bucket name.
- Day 21 of Dunning runs the same deletion for every Storage of the Team, with the Package stopped
  at the event's time.

### Billing status and the Pause

- `no_payment_method` refuses opening a Storage and any change of Package; existing Storages keep
  working.
- `suspended` stops every Storage of the Team; `active` resumes those that are not Paused.
- The operator's `pause` command stops one Storage and records the time and a reason code
  (`abuse_complaint`, `malware`, `phishing`, `legal`); `unpause` clears it and resumes the Storage
  unless its Team is suspended.
- A Storage is stopped exactly when it is Paused or its Team is suspended. That is desired state in
  the database; the request applies it to the engine and the reconcile job repeats it until the
  engine agrees.

### Events and live updates

- To billing: `StoragePackageStarted` (Storage, Team, Project, Project name, Bucket name, Region,
  Package code, size in bytes, monthly price as a two-decimal string, start) and
  `StoragePackageStopped` (Storage, Team, the start of the interval it closes, stop).
- To notification: `StorageNearlyFull`, `StorageFull`, `StoragePaused`, `StorageDowngradeSkipped`,
  each with the Team's, the Project's and the Storage's ids, the Project's name, the Bucket name and
  the thing's own fields (Package, size and used; reason code; the Package that was not applied).
  Never a recipient list (ADR-0008).
- Realtime (ADR-0007), to the Team with `storage.read`: `storage.changed`; the web app refetches.
- All through the outbox after commit, routing keys on the common exchange, queues prefixed
  `storage.`.

### Billing

- Consumes `StoragePackageStarted` and `StoragePackageStopped` into intervals per Storage, in a
  table of their own, with the same replay rules as Mail's intervals (a replay never reprices, a
  stop keeps the earliest time, a stop may arrive before its start).
- The rule "the highest monthly price among the intervals overlapping the Period, less what earlier
  Invoices of the Period already charged" exists once and is used for both a Project's Mail and a
  Storage; only what the intervals are keyed by differs.
- Invoice lines gain the kind `storage`, carrying the Storage's id, its Bucket name, the Project and
  the Package code; the PDF shows "Storage · <Bucket name> · <Package>" and the DTO carries the same
  for the web app to word.
- Threshold totals include Storage lines; tax applies as to the other lines; the set of Teams with
  usage in a Period includes Teams with only Storage.
- The Dunning warnings' wording, in `tr` and `en`, names what the Team's Storages hold beside its
  Droplets.

### Notification

- Four Notification types, in-app and email, for the members holding `storage.read`:
  `storage_nearly_full`, `storage_full`, `storage_paused`, `storage_downgrade_skipped`. Params carry
  ids, names, numbers and reason codes; the in-app text is the web app's, the email templates exist
  in `tr` and `en` and link to the Storage in the web app.

### Permissions

- `pkg/authz` gains `storage.read` and `storage.write`. owner: both; developer: both; finance:
  `storage.read`. The set of Permissions an API key may carry does not change.
- The web app's copy of the matrix gets the same change.

### Management API (storage service, gateway target `storage`)

- `GET /v1/storage/regions`, `GET /v1/storage/packages`
- `GET|POST /v1/projects/:projectID/storages`
- `GET|DELETE /v1/projects/:projectID/storages/:storageID`
- `PUT /v1/projects/:projectID/storages/:storageID/package`
- `GET|POST …/storages/:storageID/access-keys`, `DELETE …/access-keys/:accessKeyID`
- `GET …/storages/:storageID/objects`, `POST …/objects/delete`, `POST …/objects/download-url`,
  `POST …/objects/upload-url`
- `PUT …/storages/:storageID/browser-access` (restore the CORS rule)
- `GET …/storages/:storageID/usage` (size and traffic per day)

Reads need `storage.read`, writes `storage.write`; a stranger gets 404 and a member without the
Permission 403, checked by the service through the Project access read model. The customer
gateway's four route tables gain the `storage` upstream with these routes; the integration gateway
gains nothing.

### Web app

- The App switcher offers Droplets, Mail and Storage. Storage's side menu: Storages; inside a
  Storage: Files, Access keys, Usage, Settings.
- Pages: the list with a fullness bar and state per Storage; the opening form with live Bucket name
  rules, Region, Package and price; Files with folders, paging, upload, download and delete; Access
  keys with the secret key shown once; Usage with size and daily traffic; Settings with the
  connection details and copy buttons, the Package change with its schedule, and deletion by typing
  the Bucket name. Banners for suspended, Paused (with the reason), full and deleting. Every text in
  `tr` and `en`; `storage.changed` refetches.

### Errors

- The storage aggregates take rerror domains 60–69. Statuses to expect: 400 for a malformed or
  reserved Bucket name and an unknown Package or Region, 402 without a payment method, 403 and 404
  from access, 409 for a taken Bucket name, a Package the Storage does not fit, a mismatched
  deletion name and a limit on Storages or Access keys, 423 when suspended or Paused, 503 when the
  engine cannot be reached.

### Configuration and operations

- Every key of the storage binary exists in each tracked TOML (dev, test, alpha, prod): the engine's
  admin and S3 endpoints and admin key, the public domain Storages are addressed under, the web
  app's origin, the sealing key, the rate limit, the job intervals.
- Compose gains a single-container Ceph started from the official image by a bootstrap script kept
  with the deployment files; development addresses buckets by path. Make gains the targets to run,
  migrate, seed, document and test the new binary and to pause and unpause a Storage; a Dockerfile
  for the service and one for its migrations.
- CLAUDE.md lists the new context, binary, suite, error domains, gateway target and commands;
  CONTEXT-MAP.md gains the Storage relationships and drops "planned".

## Testing Decisions

A good test drives a public interface the way a client does and asserts what the client sees or what
another context receives: the HTTP status and body, an outbox document, the fake engine's recorded
calls, what a real S3 client is answered. It never inspects handlers or repositories.

Seams, highest first:

1. **Storage integration suite** (new, primary; mirrors the mail suite): dockertest PostgreSQL, Redis
   and Mongo; the storage server through its management API; consumers and jobs called directly with
   a controllable clock; the fake engine at the edge, whose sizes, traffic and failures the test
   sets. Scenarios cover every story above that the storage service owns: opening with its name
   rules, taken and reserved names and Billing status refusals; Access keys made, listed and
   revoked, and surviving a member's removal; Package moves up at once, down at the Period boundary,
   refused or skipped when the Storage does not fit; the 80% and full marks and their re-arming;
   the file browser's listing, URLs, deletes and refusals; deletion and the freed name; suspension,
   recovery and day 21; Pause, unpause and the two together; an engine that fails and is caught up
   by the reconcile job; access for owner, developer, finance and strangers; the events billing and
   notification receive.
2. **Billing suite** (existing): Storage intervals to one line per Storage and Period at the highest
   price, the Threshold difference, a Storage opened late or deleted early in a Period charged in
   full, no charge after day 21, Droplet and Mail lines unchanged.
3. **Notification suite** (existing): the four types reach `storage.read` holders, finance included,
   in-app and by email in both languages.
4. **Customer gateway suite** (existing): the `storage` target's routes exist in all four tables and
   nothing unlisted passes.
5. **Ceph contract test** (new, opt-in by environment variable like the KumoMTA and Provider
   contract tests): a real Ceph container, the real adapter and a real S3 client using the keys the
   adapter issued. It proves what only Ceph can answer: a read key cannot write; a key cannot reach
   another Storage; creating a bucket is refused; a write over the size limit is refused; a stopped
   Storage refuses keys and anonymous reads of a published object and comes back with its policies
   intact; a deleted Storage is gone with its versions; size and traffic are read back.

Unit tests, table-driven where the input is a matrix: Bucket name validation, the Package change
decision, the marks and their re-arming, the policy documents for each kind of Access key, the
highest-price-in-Period rule for both products.

Prior art: the mail suite (fake MTA, jobs called directly, controllable clock, outbox assertions),
the billing suite, the notification suite, the customer gateway suite, the KumoMTA contract test.
The web app has no automated UI tests; type-check, lint and build guard it.

## Out of Scope

- The production cluster: ordering servers, installing Ceph, monitoring it, the wildcard
  certificate and the DNS of the Storage domain. It follows Cherry's quote (ADR-0012).
- Selling or limiting transfer per Package, a CDN, a second Region, replication to a second site and
  any durability figure or SLA.
- Static website hosting, bucket notifications and a customer's own domain on a Storage.
- Opening, changing or deleting a Storage with an API key or from Terraform; opening a bucket over
  S3.
- An Access key that reaches several Storages; Access key expiry and rotation.
- In the file browser: moving and renaming, folders as things of their own, previews, a
  publish/unpublish switch, multipart uploads of files over 5 GB.
- The Admin side: Pausing, lifting a Pause and changing the catalog are commands and table edits
  until then. Scanning stored files for malware.
- Keeping Nodera's own Invoice PDFs in Storage.

## Further Notes

- Before Storage is sold: Cherry's quote (16 TB disks, whether a custom server bills hourly, a second
  network card, whether private traffic counts against the allowance) and the catalog prices that
  follow from it; a registered domain for Storage; an abuse@ mailbox and a Storage clause in the
  terms of service binding customers to what Cherry's terms forbid storing.
- The engine and hosting research behind ADR-0012 and the provisional catalog dates from 2026-10-06.
  At that date the Go client for Ceph's admin API kept bucket rate limits behind a preview build
  tag and did not cover the IAM API, which is reached with an AWS IAM client instead.
- An Access key is made for one Storage in this phase, but all of a Project's Storages share one
  Ceph account, so letting a key reach several later is a change of its policy, not of the model.
