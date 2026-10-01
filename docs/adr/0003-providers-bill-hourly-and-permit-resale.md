---
status: accepted
---

# A Provider must bill by the hour and permit resale; Cherry Servers is the first one

Nodera charges a customer only for the hours a Droplet exists, at a per-hour Plan price with a
monthly cap, and the Plan price is exactly the Provider's price plus Nodera's Commission. Both
rules only hold if every Provider behind a Plan (a) bills Nodera by the hour through its API and
(b) contractually allows Nodera to resell the server to its own customers. A Provider that fails
either test is not integrated, however good its footprint.

Applied on 2026-10-01 to the candidates we researched:

- **Zenlayer** (the business's first choice): the API supports `POSTPAID`, but the public docs
  reserve pay-as-you-go for "VIP customers" and its MSA (3.2(i)) requires written consent before
  third-party end users may use the service. Deferred until both are granted in writing; the
  adapter will be added then, using `POSTPAID` only.
- **Latitude.sh**: hourly, official Go SDK, but its terms are the Megaport Global Services
  Agreement, whose clause 7(b)(iv) reads "not resell or resupply the Service". Rejected.
- **DataPacket**: monthly only and resale explicitly refused. Rejected.
- **Hivelocity**: hourly and a formal reseller programme, but no SDK, a two-server cap and a
  one-month deposit for new accounts. Kept as a later candidate.
- **Cherry Servers**: server creation defaults to `cycle: hourly`, official Go SDK
  (`github.com/cherryservers/cherrygo/v4`, MIT), and its terms of service state in 2.4 "You may
  resell the Services". Regions in Lithuania, the Netherlands, Germany, Sweden, Singapore and
  Tokyo. **Chosen as the first Provider.**

## Consequences

- Cherry Servers draws hourly charges from a prepaid account balance, so operating Nodera means
  keeping that balance topped up; an empty balance surfaces as a failed order, which the order
  flow must report honestly.
- The Provider adapter interface is drawn against Cherry Servers and Zenlayer's `POSTPAID` API,
  so the Zenlayer adapter is an addition, not a redesign.
- Region coverage at launch is Cherry's; Istanbul waits for Zenlayer's approval.
- A Plan never carries a monthly-only price. If a Provider one day offers a cheaper committed
  term, that is a Nodera purchasing decision behind the same hourly Plan, never a customer-facing
  billing cycle.
