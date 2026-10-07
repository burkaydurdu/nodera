# Storage

The context behind Nodera's third product: a Team's software keeps its files on Nodera's own
storage servers and reaches them over the S3 protocol. It knows a Project only by its identifier;
Teams, Users and Roles belong to the customer context.

## Language

### Keeping files

**Storage**:
What a customer opens to keep files in: one S3 bucket on Nodera's own storage servers, with no
supplier behind them, under a Bucket name, in a Region and on a Package. Belongs to exactly one
Project. The product and its App of the web app carry the same name.
_Avoid_: Space (another company's word), bucket (bare; what a Storage is on the wire), disk,
volume, storage provider (a Provider is a supplier Nodera resells; Storage has none)

**Bucket name**:
The name software reaches a Storage by over S3, chosen when the Storage is opened and never
changed. No two Storages in a Region share one, whichever Teams they belong to; a deleted
Storage's name is free again.
_Avoid_: name (bare), slug, hostname

**Region**:
A place Nodera keeps Storage in, named by the same codes as the droplet context's Regions (such as
`fra1`) and part of the address software reaches it at, so what is kept there never moves to
another.
_Avoid_: zone, location, datacenter, endpoint (the address, not the place)

**Access key**:
What software presents to reach a Storage over S3: an access key ID and the secret key its
requests are signed with, shown once when the Access key is made. Made for one Storage, to read it
or to read and write it, and it outlives the member who made it. Nothing in a Storage is read or
written without one, except an object its owner chose to publish.
_Avoid_: API key (the customer context's, for the integration gateway), Storage key, credential,
token

### Limits

**Package**:
The size a Storage may hold, such as 10 GB or 250 GB, and its monthly price, chosen from Nodera's
catalog. A full Storage takes no more writes, while reading and deleting go on; what leaves it
over the network is not part of the Package. The Team's Invoice for a Period carries, for each
Storage, the price of the largest Package it held in that Period, never prorated.
_Avoid_: plan (the Droplet catalog's word), subscription, tier, quota, size (bare)

**Pause**:
Nodera stopping a Storage after a complaint about what it holds or publishes: nothing is read or
written, published objects included, while what is stored stays. Only an operator lifts it.
_Avoid_: suspension (billing's word, for an unpaid Invoice), block, ban, takedown

**Billing status**:
What the billing context last said about a Team: `no_payment_method` (the default for a Team never
heard about), `active` or `suspended`. Learned from events, never queried. No payment method
refuses anything new that would be charged; suspended also stops reading and writing while what is
stored stays; what a Team still suspended has stored is deleted for good on the day its Droplets
are.
_Avoid_: account status, blocked, frozen
