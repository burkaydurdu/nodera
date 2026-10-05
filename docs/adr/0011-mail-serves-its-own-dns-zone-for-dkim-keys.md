---
status: accepted
---

# Mail serves its own DNS zone for the DKIM keys behind a Team's CNAMEs

A Sending domain is verified the way SendGrid does it: the Team adds three CNAME records, one for the
return path and two DKIM selectors, and Nodera owns everything behind them, so it can change a key
without asking the Team for anything. Each Project's DKIM keys are its own, so the record a CNAME
points at differs per Sending domain and changes on every key rotation. Something must publish those
records. We chose to have the mail service answer them itself, from its own database, as the
authoritative server of a zone delegated to it (`mail dns`). We weighed a DNS provider's API, which
would make the zone's correctness depend on a third party's API calls succeeding at the right moment
and on credentials to the platform's whole DNS, and plain DKIM TXT records pasted by the Team, which
would make every key rotation the Team's chore and break SendGrid's model.

## Consequences

- Operating Mail needs an NS delegation of the zone from Nodera's DNS and the `dns` process running on
  at least two hosts, reachable on UDP and TCP port 53.
- The zone also carries the static records every return path CNAMEs to: the MX of Nodera's bounce and
  feedback-loop listener and the SPF record listing the sending IPs, all from configuration.
- A key rotation publishes the new key under the idle selector at once and signs with it only after a
  waiting period, so a resolver's cached copy of the old record never meets a signature it cannot
  check.
- No DNSSEC at launch.
