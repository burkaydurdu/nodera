---
status: accepted
---

# Billing: postpaid monthly invoices charged through Craftgate, with an e-invoice-ready model

Nodera prices in USD, buys its servers prepaid, and sells to Turkish and international Teams
from a Turkish company. Three facts decided the shape of billing on 2026-10-01:

1. **Stripe is not available to Turkish entities.** International cards therefore go through
   Craftgate as well, over a Turkish bank's USD virtual POS, until a non-Turkish entity exists.
   The country → Payment provider rule is a table; both rows point at Craftgate today, and the
   Stripe adapter is an addition behind the same interface.
2. **Craftgate is an orchestration layer, not a payment institution.** Nodera needs its own
   bank POS contracts (TRY and USD), one of them enabled for merchant-initiated charges without
   3-D Secure. Cards are saved through Craftgate's hosted card verification so card numbers never
   reach Nodera; monthly charges run without 3-D Secure, and a bank refusal falls back to a
   confirmation link the cardholder completes.
3. **A PDF is not a legal invoice in Türkiye.** From 2026 balance-sheet taxpayers must issue
   e-Arşiv invoices through the tax authority or an integrator. Nodera's Invoice is therefore a
   statement until an e-invoice issuer adapter is connected, but its model already carries what
   the integrator will ask for: account type, legal name, tax identifiers and office, address,
   per-line tax, the TRY equivalent and the central bank rate of the issue date.

The payment model is postpaid: a Team must hold a verified Payment method before its first
Droplet, usage accumulates in a draft Invoice per calendar month, the Invoice is issued and
charged at month end or when a Threshold is crossed, and failed Payments run a fixed Dunning
schedule that suspends and eventually deletes the Team's Droplets. Credit exists in the ledger
so prepaid top-ups can be sold later without a model change.

## Consequences

- Nodera carries the cost between buying a server and collecting the Invoice; the Payment method
  requirement and the lower Thresholds for new Teams bound that exposure.
- The droplet context never asks billing anything: it learns `no_payment_method`, `active` and
  `suspended` from events and refuses or stops Droplets accordingly.
- Tax rules are data managed by operators, not code, because rates and exemptions differ by
  country and account type and will change; the export-service exemption for non-Turkish Teams
  and the 20% KDV for Turkish Teams are the first two rows, to be confirmed with the accountant.
- The TRY amount is fixed by the issue-date rate even when a retry collects days later; exchange
  differences stay with Nodera rather than appearing on the customer's statement.
- When a non-Turkish entity exists, switching international Teams to Stripe is a change to the
  routing table and an adapter, not to the Invoice or Payment model.
