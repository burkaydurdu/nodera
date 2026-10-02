# Billing

The context that turns a Team's Droplet hours into money: what the Team owes, how it pays,
and what it is told about both. It knows a Team by its identifier and a Droplet by the usage
events the droplet context publishes; it never reads either context's tables.

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
Whether a Team may spend: `no_payment_method` (nothing can be created), `active`, `suspended`
(an unpaid invoice stopped its Droplets). The droplet context learns it from events.
_Avoid_: account status, blocked, frozen

### Usage and money

**Usage**:
The hours a Droplet was rented in a period, counted from the moment it became active until its
deletion was requested, powered off or not, rounded up to whole hours and capped at 672 per
Droplet per period. Priced at the Droplet's Locked price.
_Avoid_: consumption, metering, meter (the mechanism, not the thing)

**Period**:
A calendar month in UTC. Usage is grouped and invoiced per Period.
_Avoid_: billing cycle, month (ambiguous), term

**Invoice**:
A Team's statement for a Period or a Threshold: one Invoice line per Droplet, the Tax lines, any
Credit applied, totals in USD and, for Turkish Teams, the TRY equivalent at the Exchange rate of
its issue date. It moves `draft` → `issued` → `paid` or `payment_failed` → `void`. It is numbered
on issue. Until an e-invoice issuer is connected it is a statement, not a legal invoice.
_Avoid_: bill, receipt (that is the proof of a Payment), statement (what it legally is today,
but one word in the product)

**Invoice line**:
One priced row of an Invoice: a Droplet's hostname, Plan, hours, hourly price and amount.
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
deletion of the Team's Droplets on day 21, with warnings before each step.
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
