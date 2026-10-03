# Droplet

The context that turns a customer's request into a running bare-metal server. It owns the
catalog the customer buys from, the Providers the platform rents from, and the price the
customer sees. It knows a Project only by its identifier; Teams, Users and Roles belong to
the customer context.

## Language

### Catalog

**Region**:
A place Nodera sells Droplets in, named by Nodera's own code (such as `fra1`) and shown to
the customer as a city and country. A Region maps to one or more Provider locations; the
customer never learns which.
_Avoid_: zone, PoP, datacenter, location (those are the Provider's words)

**Plan**:
A Droplet configuration a customer can buy in a Region: CPU, RAM, disk, included bandwidth,
one public IPv4, and its price. Backed by exactly one Offer per Region.
_Avoid_: size, instance type, SKU, flavor, tier

**Offer**:
A Provider's own listing of a server it will rent: its identifier, location, hardware, price
and stock, exactly as the Provider's API reports it. Never shown to a customer.
_Avoid_: plan (that is what the customer sees), product, inventory item

**Commission**:
The share Nodera adds on top of an Offer's price to arrive at the Plan's price. Managed by
Nodera, never disclosed to the customer.
_Avoid_: markup, margin, fee, surcharge

### Droplets

**Droplet**:
What a customer buys: a bare-metal server matching the Plan, Region and Image the customer chose,
rented from a Provider and set up by the platform. Belongs to exactly one Project. Shared with
the customer context, which uses the same word.
_Avoid_: server, instance, VM, machine, order

**Image**:
The operating system a Droplet is installed with, named by Nodera's own code (such as
`ubuntu-24-04`) and mapped to each Provider's own image behind the scenes.
_Avoid_: OS, template, distro

**Droplet status**:
Where a Droplet is in its life: `provisioning` (asked of the Provider, not yet usable), `active`
(running and reachable), `off` (powered down, still rented), `failed` (the Provider could not
deliver it), `deleting`, `deleted`. Only `active` and `off` count as usage.
_Avoid_: state (the Provider's word), phase

**Failure code**:
Why a Droplet failed: `rejected` (the Provider refused the order), `unavailable` (the Provider
could not be asked), `timed_out` (never delivered), `server_gone` (the server vanished),
`provider_failed` (the Provider reported it failed). The words for it are the reader's language;
the failure reason is the English sentence kept for the history.
_Avoid_: error code, failure type

**Locked price**:
The Plan's hourly price copied onto a Droplet when it is created. It never changes for that
Droplet, whatever happens to the Plan or the Offer afterwards.
_Avoid_: current price, list price

**Project access**:
What the droplet context knows about a Project: which Team it belongs to and which Users are
members with which Role. Learned from the customer context's events, never queried. The Role
is checked against the shared matrix (`pkg/authz`): `droplet.read` to list and view,
`droplet.write` to create, act on and delete. The events arrive unordered, so each row
follows the newest event by its own time, and a removed member is remembered as removed
rather than deleted, so a late copy of an older event cannot bring them back.
_Avoid_: membership (the customer context's word), permission

### Billing

**Billing status**:
What the billing context last said about a Team: `no_payment_method` (the default for a Team
never heard about), `active` or `suspended`. Learned from events, never queried. No payment
method refuses creating a Droplet; suspended refuses creating and powering on, and powers off
the Team's running Droplets until the status is `active` again.
_Avoid_: account status, subscription, plan (that is the catalog's word)

**Meter**:
The usage interval the billing context charges a Droplet for: it starts the first time the
Droplet is `active` and stops when its deletion is requested, or when it fails after having
run. Each boundary is announced to the billing context exactly once.
_Avoid_: uptime, billing period (that is the billing context's word)

### Providers

**Provider**:
An integrated bare-metal supplier the platform rents physical servers from through its API.
Never visible to a customer. Shared with the customer context; defined there as well.
_Avoid_: vendor, supplier, upstream, datacenter
