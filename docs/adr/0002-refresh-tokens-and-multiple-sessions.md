---
status: accepted
---

# Access + rotating refresh tokens, several sessions per user

The reference repos (apex-admin, apex-custody) issue one 24-hour JWT per login, keep its HMAC in
Redis and allow a single active session. Nodera instead issues a 15-minute access JWT plus a
30-day refresh token, rotates the refresh token on every use, revokes the whole session when a
rotated-out token is presented again, and lets a User hold one session per device with
`logout` / `logout-all`. A self-service cloud customer stays signed in for weeks from several
devices, which a single day-long token cannot give without either constant re-login or a
long-lived bearer that cannot be revoked per device.

## Consequences

- The access JWT carries a `sid` claim. The customer gateway checks `session:{sid}` in Redis on
  every authenticated request so logout is effective at once, at the cost of one Redis read per
  request (the same cost the reference gateways already pay for their token-hash check).
- The frontend's axios client must refresh on 401 and retry once; the reference clients only wipe
  the session.
