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

**Locked price**:
The Plan's hourly price copied onto a Droplet when it is created. It never changes for that
Droplet, whatever happens to the Plan or the Offer afterwards.
_Avoid_: current price, list price

**Project access**:
What the droplet context knows about a Project: which Team it belongs to and which Users are
members. Learned from the customer context's events, never queried.
_Avoid_: membership (the customer context's word), permission

### Providers

**Provider**:
An integrated bare-metal supplier the platform rents physical servers from through its API.
Never visible to a customer. Shared with the customer context; defined there as well.
_Avoid_: vendor, supplier, upstream, datacenter
