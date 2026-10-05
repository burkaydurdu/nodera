# Billing

The context that turns a Team's Droplet hours and Mail Packages into money: what the Team owes,
how it pays, and what it is told about both. It knows a Team by its identifier, a Droplet by the
usage events the droplet context publishes and a Project's Package by the events the mail context
publishes; it never reads another context's tables.

## Language

### The paying side

**Billing profile**:
Who the invoice is made out to: a Team's account type (individual or company), legal name,
country, address, tax identifiers and the email invoices go to. A Team must have one before it
can hold a Payment method.
_Avoid_: customer details, billing address (that is one field of it), account

**Payment method**:
A card a Team has saved for Nodera to charge, held as the Payment provider's token with its
brand, last four digits and expiry. A Team may hold several; one is the default. Nodera never
holds the card number.
_Avoid_: card (bare), credit card, token

**Payment provider**:
The service Nodera charges cards through: Craftgate today for TRY and USD cards, Stripe when a
non-Turkish entity exists. Chosen by the Team's billing country; never shown to the customer.
_Avoid_: PSP, gateway (that is Nodera's own API gateway), processor

**Billing status**:
Whether a Team may spend: `no_payment_method` (no Droplet can be created and no Package chosen),
`active`, `suspended` (an unpaid invoice stopped its Droplets and its Mail sending). The droplet
and mail contexts learn it from events.
_Avoid_: account status, blocked, frozen

### Usage and money

**Usage**:
What a Team used in a Period and pays for. For a Droplet, the hours it was rented, counted from
the moment it became active until its deletion was requested, powered off or not, rounded up to
whole hours, capped at 672 per Droplet per period and priced at the Droplet's Locked price. For
Mail, the Packages each Project held.
_Avoid_: consumption, metering, meter (the mechanism, not the thing)

**Package**:
The Mail allowance a Project is on, as the mail context reports it: intervals from a start to a
stop, each at the monthly price the Package was chosen at. A Period charges a Project the highest
monthly price among the intervals it held in that Period, never prorated, less what earlier
Invoices of the Period already charged for that Project.
_Avoid_: plan (the Droplet catalog's word), subscription, tier

**Period**:
A calendar month in UTC. Usage is grouped and invoiced per Period.
_Avoid_: billing cycle, month (ambiguous), term

**Invoice**:
A Team's statement for a Period or a Threshold: one Invoice line per Droplet and per Project's
Package, the Tax lines, any Credit applied, totals in USD and, for Turkish Teams, the TRY
equivalent at the Exchange rate of its issue date. It moves `draft` → `issued` → `paid` or
`payment_failed` → `void`. It is numbered on issue. Until an e-invoice issuer is connected it is a
statement, not a legal invoice.
_Avoid_: bill, receipt (that is the proof of a Payment), statement (what it legally is today,
but one word in the product)

**Invoice line**:
One priced row of an Invoice, of one kind: a Droplet line carries the Droplet's hostname, Plan,
hours, hourly price and amount; a Mail line the Project, its name, the Package and the amount.
_Avoid_: item, entry

**Threshold**:
A usage amount within a Period that triggers an early Invoice for a Team, lower for new Teams.
_Avoid_: limit, cap (that is the 672-hour cap), quota

**Payment**:
One attempt to collect an Invoice from a Payment method, with the Payment provider's result:
succeeded, failed, or awaiting the cardholder's confirmation (3-D Secure).
_Avoid_: charge, transaction, collection

**Dunning**:
The retries and emails after a failed Payment: retries on days 1, 3 and 7, suspension on day 7,
deletion of the Team's Droplets and the end of its Packages on day 21, with warnings before each
step.
_Avoid_: collections, reminder flow

**Credit**:
Money a Team holds with Nodera ahead of use, applied to an Invoice before its card is charged.
Modelled now, sold later.
_Avoid_: balance, wallet, prepaid (the model, not the thing)

**Tax rule**:
What tax applies to a Team: a rate and a label chosen by billing country and account type,
managed by Nodera's operators, applied as Tax lines on an Invoice.
_Avoid_: VAT (one tax among others), tax code

**Exchange rate**:
The Turkish central bank's buying rate for USD on a given day, stored daily and used for the TRY
equivalent of an Invoice issued that day.
_Avoid_: FX, conversion, kur (in code)
