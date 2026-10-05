# Mail

The context behind Nodera's second product: a Team's software hands Nodera its email and Nodera
delivers it from its own mail servers. It knows a Project only by its identifier; Teams, Users and
Roles belong to the customer context.

## Language

**Mail**:
Nodera's product for sending a Team's email from the Team's own domains, delivered by Nodera's own
mail servers with no supplier behind them (ADR-0009). Lives in a Project and is an App of the web
app.
_Avoid_: email (the notification Channel), mail provider (a Provider is a supplier Nodera resells;
Mail has none), SMTP service

### Sending

**Sending domain**:
A domain a Project sends from, once the Project has proven through DNS that it controls it. Mail
leaves only from a Sending domain. The same domain may be a Sending domain of several Projects,
each proving it on its own.
_Avoid_: domain (bare), identity, sender

**Message**:
One email a Project's software hands to Nodera for one or more recipients. Each recipient of an
accepted Message counts once against the Package, delivered or bounced; a Message refused at the
door counts nothing.
_Avoid_: email (bare), mail (the product), send

**Message event**:
Something that happened to one recipient of a Message: `accepted`, `delivered`, `deferred`,
`bounced`, `suppressed` or `complained`. What the logs show and Webhooks carry.
_Avoid_: status (bare), activity, delivery (one event among the others)

**Recipient status**:
Where one recipient of a Message stands: the furthest Message event it has reached, so a late
deferral never undoes a delivery and a suppressed recipient stays suppressed. What the logs list
and filter by.
_Avoid_: last event (it is not always the latest), delivery status

**Webhook**:
An address on the Team's own server to which Nodera sends a Project's Message events, signed so
the Team can trust them.
_Avoid_: callback, hook, event webhook (SendGrid's name)

**Suppression**:
An address a Project no longer sends to, because mail to it bounced for good or its recipient
complained. Kept per Project.
_Avoid_: blocklist (that names bad senders, not recipients), bounce list, unsubscribe

### Limits

**Package**:
The monthly allowance of email a Project is on, such as 5,000 or 10,000, and its price. The
allowance renews each Period (a calendar month in UTC); the Team's Invoice for a Period carries the
price of the largest Package the Project held in it; when the allowance runs out, the Project's
sending stops.
_Avoid_: plan (the Droplet catalog's word), subscription, tier, quota, credit

**Daily limit**:
The most email a Project may send in one UTC day. It starts low for a new Project and rises while
its mail is delivered without bounces and complaints, never beyond what its Package has left.
_Avoid_: quota, rate limit, warm-up (that is an IP's)

**Pause**:
Nodera stopping a Project's sending because its bounce or complaint rate crossed the line. Only an
operator lifts it.
_Avoid_: suspension (billing's word, for an unpaid Invoice), block, ban
