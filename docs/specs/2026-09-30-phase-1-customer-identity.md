# Phase 1: customer identity — sign-up, sign-in, MFA, profile, first project

Vocabulary: `contexts/customer/CONTEXT.md`. Decisions already recorded: ADR-0001 (MongoDB only for
the outbox), ADR-0002 (access + rotating refresh tokens, several sessions per User).

## Problem Statement

Nodera has no product yet. A person who wants a Droplet cannot create an account, prove their
email is theirs, sign in safely from more than one device, or set up the Team and Project their
Droplets will later live in. Everything that comes after (ordering a Droplet, billing, an admin
panel) needs a User who exists, is verified, and is signed in.

## Solution

A self-service identity surface for the customer side of Nodera, built as the first slice of the
platform's architecture rather than a throwaway:

- A **customer service** exposing sign-up with email verification, sign-in with an email MFA
  step, refreshable sessions, password recovery, a profile (name, Locale, Country, password, MFA
  method, sessions), and the Team plus Project a User works inside.
- A **customer gateway** in front of it: deny-by-default route table, JWT and session check.
- A **notification worker** that turns the customer service's email events into emails in the
  User's Locale.
- A **web app** (`nodera-client`) with the sign-up, verification, sign-in, MFA, password-recovery
  screens, a profile page, and a dashboard whose first step is creating a Project.

## User Stories

### Sign-up and verification

1. As a visitor, I want to sign up with my email, a password, my name and my Country, so that I
   get a Nodera account.
2. As a visitor, I want the sign-up form to tell me live which password rules I still miss, so
   that I do not submit a password that will be rejected.
3. As a visitor, I want the app to pick my Locale from the language I am reading it in, so that I
   am not asked what I can already see.
4. As a visitor, I want the sign-up response to look the same whether or not the email is already
   registered, so that nobody can use the form to discover who has an account.
5. As a person whose email was used to sign up again, I want an email telling me the address is
   already registered, so that I know and can sign in or reset my password instead.
6. As a new User, I want a 6-digit code emailed to me, so that I can prove the address is mine.
7. As a new User, I want to enter that code on a verification screen, so that my account becomes
   active.
8. As a new User, I want to be signed in right after a correct verification code, so that I do
   not have to type the same code again at the MFA step a second later.
9. As a new User, I want to request the code again if it never arrived, so that I am not stuck,
   and I accept a short wait between requests.
10. As a new User, I want a wrong code to be refused with the number of attempts I have left, so
    that I know when I must request a fresh one.
11. As a new User, I want an expired or exhausted code to stop working, so that a leaked code
    cannot be used later.
12. As an unverified User, I want sign-in to send me to the verification screen instead of
    refusing me, so that I can finish what I started.

### Sign-in and MFA

13. As a User, I want to sign in with email and password, so that I reach my dashboard.
14. As a User with email MFA on, I want a code emailed to me after a correct password, so that a
    stolen password alone does not open my account.
15. As a User, I want the MFA screen to say which method is being used and whether I can resend,
    so that I know what to expect.
16. As a User, I want to resend the MFA code after a short cooldown, so that a lost email does not
    lock me out.
17. As a User, I want the MFA challenge to expire after a few minutes and after too many wrong
    codes, so that guessing is not possible.
18. As a User with MFA off, I want sign-in to give me my session directly, so that I am not made
    to wait for a code I chose not to have.
19. As a User, I want a wrong email or wrong password to be answered identically, so that the
    sign-in form cannot be used to discover who has an account.
20. As a User, I want repeated failed sign-ins to lock my account for a growing period, so that a
    brute-force attempt is slowed down to nothing.
21. As a User, I want the lock response to tell me how long to wait, so that I do not keep trying
    and extend the lock.
22. As a suspended User, I want sign-in to tell me the account is suspended, so that I contact
    support instead of assuming a wrong password.
23. As the platform, I want the public sign-in, sign-up and code endpoints rate-limited per source
    address and per email, so that one client cannot flood them.

### Sessions

24. As a User, I want to stay signed in for weeks on a device I use, so that I do not sign in
    every day.
25. As a User, I want my session to renew itself silently while I use the app, so that I never
    see a sign-in screen mid-task.
26. As a User, I want to be signed in on my laptop and my phone at the same time, so that either
    device works.
27. As a User, I want signing out on one device to leave my other devices signed in, so that
    signing out of a shared computer costs me nothing elsewhere.
28. As a User, I want a "sign out everywhere" action, so that a lost device is cut off at once.
29. As a User, I want to see my active sessions with device, address and last-use time, so that I
    can spot one I do not recognise and end it.
30. As a User, I want signing out to take effect immediately on every request that follows, so
    that an ended session cannot be used, even before its access token expires.
31. As the platform, I want a refresh token that is presented a second time after it was rotated
    to end that whole session, so that a copied token is useless to a thief and the User is
    signed out on the device that was copied.

### Password recovery and change

32. As a User who forgot the password, I want to request a reset link by email, so that I can get
    back in.
33. As a visitor, I want the forgot-password response to look the same whether or not the email
    exists, so that it cannot be used for discovery.
34. As a User, I want the reset link to expire and to work only once, so that an old email in my
    inbox is not a permanent way in.
35. As a User, I want resetting my password to sign me out everywhere, so that whoever made me
    reset it is signed out too.
36. As a signed-in User, I want to change my password by giving the current one, so that a
    session left open cannot silently change it.
37. As a User, I want changing my password to end my other sessions but keep the one I am using,
    so that I am not signed out of the device I am on.

### Profile, Locale, Country, MFA

38. As a User, I want to see my email, name, Locale, Country, MFA method and account status on a
    profile page, so that I know what Nodera holds about me.
39. As a User, I want to edit my name, Locale and Country, so that they stay correct.
40. As a User, I want emails from Nodera to arrive in my Locale, so that I can read them.
41. As a User, I want to switch the app language without signing out, so that a wrong language
    is a one-click fix.
42. As a User, I want to turn email MFA off, giving my password to confirm, so that I can choose
    convenience.
43. As a User, I want to turn email MFA back on, so that I can choose safety later.
44. As a new User, I want email MFA to be on from the start, so that the safe choice is the
    default.

### Team and Project

45. As a new User, I want a Team to exist for me the moment I sign up, so that I never have to
    set one up before doing anything.
46. As a User, I want to be the owner of the Team created for me, so that nothing in it is
    beyond my control.
47. As a User signing in with no Project, I want the dashboard to ask me to create my first
    Project, so that the next step is obvious.
48. As a User, I want to create a Project with a name and a description inside my Team, so that
    my Droplets will have somewhere to live.
49. As a User, I want to see the Projects of my Team, so that I can pick one to work in.
50. As a User, I want a Project name to be unique inside its Team, so that two Projects cannot
    be told apart only by id.
51. As a User, I want the app to remember which Project I last worked in, so that I land there
    next time.
51a. As a User, I want to create another Team with a name of my choosing, so that unrelated
    work does not share one Team.
51b. As a User, I want to switch between my Teams on the dashboard, so that I see and create
    the Projects of the Team I am working in.

### Gateway

52. As the platform, I want every customer-facing route to pass through one gateway with a
    deny-by-default route table, so that a route not deliberately published does not exist.
53. As the platform, I want the gateway to reject an access token whose session has ended, so
    that revocation is immediate.
54. As the platform, I want the gateway to require the target-service header, so that adding the
    admin service later needs no client change.
55. As a front-end developer, I want the gateway to return the same error envelope as the
    services, so that one error handler serves all.

### Operations and development

56. As a developer, I want the whole stack (databases, broker, mail catcher) to start with one
    command, so that I can work locally in minutes.
57. As a developer, I want a seeded, verified dev User, so that I can sign in without going
    through sign-up each time.
58. As a developer, I want a fixed verification/MFA code in the dev environment, so that manual
    testing does not depend on reading mail.
59. As a developer, I want every verification, MFA and reset email visible in a local mail
    catcher, so that I can check content and Locale by eye.
60. As a developer, I want migrations and seeds to be versioned files applied by one command, so
    that a database can be rebuilt from nothing.
61. As a developer, I want the API documented as Swagger from the code, so that the front-end and
    the back-end agree on shapes.
62. As an operator, I want each binary to build into its own container image, so that deployment
    later needs no restructuring.

## Implementation Decisions

### Repositories and shape

- Two repositories: the Go backend (this one, module `github.com/piggystats/nodera`, Go 1.27) and
  `nodera-client` (Next.js 16 App Router, React 19, antd 6, Tailwind 4, next-intl) in the sibling
  directory. Each is its own git repository; commits are the user's.
- The backend follows the apex-custody skeleton: `cmd/<binary>` (cobra root, `--env` selects a
  TOML under `.conf/`, every key overridable by `NODERA_<BINARY>_*` env vars, config validated at
  boot), `contexts/<ctx>/{aggregates,application/{handlers,dto},api/v1,async,db/{migrations,seed}}`,
  shared code under `pkg/` and `internal/persistence/{postgresql,redis,mongo}`, `logger/`,
  `utils/`, `templates/`.
- Binaries: `customer` (server, worker, outbox, migrate, rollback, seed, seed-rollback),
  `customer-gateway` (server), `notification` (worker). Contexts: `customer`, `gateway`,
  `notification`. Admin is a later context.
- Layering as in the reference: API layer binds and validates a DTO and delegates; application
  handlers orchestrate services and hold the business rules; each aggregate package owns its
  model, request forms, errors, thin service and hand-written pgx repository. No aggregate imports
  a sibling aggregate; no context imports another context. Transactions ride the context.
- Private libraries in use: `piggystats/rerror` (error type and envelope), `piggystats/rabbitmq`
  (broker client with message auth), `piggystats/outbox` (Mongo-backed outbox, ADR-0001).
- Ids are UUID v7 generated by PostgreSQL 18; they cross the DTO boundary as strings.
- Error envelope is `{code, domain, message}` with the HTTP status from the error's own status.
  Domains: common 100, user 1, session 2, mfa 3, verification 4, team 5, project 6, gateway 7,
  notification 8. Codes start at 10001 per aggregate.
- Empty-string checks go through a `str` helper; no `== ""`.

### Data model (PostgreSQL, database `nodera_customer`)

- `users`: id, email (unique among non-deleted), password hash (argon2id, PHC string), full_name,
  locale (`tr|en`), country (ISO 3166-1 alpha-2, validated against a fixed list), status
  (`pending_verification | active | suspended`, PostgreSQL enum), mfa_method (`none | email`,
  enum, default `email`), email_verified_at, created_at, updated_at, deleted_at.
- `teams`: id, name, owner_user_id, created_at, updated_at, deleted_at.
- `team_members`: team_id, user_id, role (`owner`, enum), created_at; unique per (team, user).
- `projects`: id, team_id, name (unique per team among non-deleted), description, created_at,
  updated_at, deleted_at.
- Sign-up creates the User, their Team (named after the User) and the owner membership in one
  transaction. Nothing is owned outside a Team.
- Soft delete everywhere; reads filter `deleted_at IS NULL`.
- Migrations and seeds are golang-migrate files with up and down; seeds use their own version
  table. The dev seed creates `dev@nodera.local` / `Nodera1234!`, active, MFA on, with its Team.

### Redis (short-lived state; every key has a TTL)

- Email verification code: per User, 6 digits, 10 minutes, at most 5 wrong attempts, resend
  cooldown 60 seconds.
- MFA email code: per User, 6 digits, 3 minutes. Login challenge: random 32-byte token → {user,
  method}, 10 minutes, at most 5 wrong attempts, resend cooldown 60 seconds. A resend replaces the
  code and keeps the challenge.
- Session: `session:{sid}` → user id, hash of the current refresh token, refresh token id, device
  string, source address, created and last-used times; TTL equals the refresh token's remaining
  life. A per-user set of session ids serves listing and "sign out everywhere".
- Password reset: random 32-byte token → user id, 1 hour, deleted on first use.
- Login lockout: per email hash, progressive schedule (5, then 3, then 1 attempts → 1, 5, 15, 30,
  60 minutes), reset window 1 hour, atomic update.
- Rate limits: sliding window per source address and per email on sign-up, verification, sign-in,
  MFA verify/resend, forgot-password.
- In `dev` the verification and MFA code is fixed to `123456`; every other environment draws
  from a cryptographic source. Codes are compared in constant time.

### Tokens (ADR-0002)

- Access token: HS256 JWT, 15 minutes, claims `sub` (user id), `sid`, `email`, `status`, `exp`,
  `iat`. Refresh token: HS256 JWT, 30 days, claims `sub`, `sid`, `jti`.
- Refresh rotates: a new refresh token is minted, its hash and `jti` replace the session's; the
  access token is re-issued. A refresh token whose `jti` is not the session's current one revokes
  the session (reuse detection).
- Sign-out deletes the session; sign-out-everywhere deletes every session of the User. Password
  reset deletes every session; password change deletes every session except the current one.
- Session-creating events: email verification success, sign-in without MFA, MFA verify success.

### Customer service API (all under `/v1`; every mutating endpoint takes JSON)

Public:
- `POST /auth/register` {email, password, full_name, country, locale} → 201, empty. Duplicate
  email still 201; the existing owner gets an "already registered" email.
- `POST /auth/verify-email` {email, code} → 200 with a session (token pair).
- `POST /auth/verify-email/resend` {email} → 200 always.
- `POST /auth/login` {email, password} → 200 either `{mfa_required: true, challenge_token,
  method, supports_resend, resend_after_seconds}` or `{mfa_required: false, session}`. Unverified
  User → a dedicated error telling the client to go to verification. Locked → 429 with
  `Retry-After`. Suspended → 403 with its own code. Wrong email or password → the same 404 code.
- `POST /auth/mfa/verify` {challenge_token, code} → 200 session.
- `POST /auth/mfa/resend` {challenge_token} → 200.
- `POST /auth/refresh` {refresh_token} → 200 session.
- `POST /auth/forgot-password` {email} → 200 always.
- `POST /auth/reset-password` {token, password, password_confirm} → 200.
- `GET /health`.

Authenticated (bearer access token; the service re-validates the JWT and the session):
- `POST /auth/logout` → 204. `POST /auth/logout-all` → 204.
- `GET /users/me` → profile plus the User's Team summary. `PUT /users/me` {full_name, locale,
  country}. `POST /users/me/password` {current_password, new_password, new_password_confirm}.
  `PUT /users/me/mfa` {method, password}. `GET /users/me/sessions` (current one flagged).
  `DELETE /users/me/sessions/:sessionID`.
- `GET /teams` (the User's Teams). `POST /teams` {name} → 201 (the caller becomes owner; the
  Team and the membership are created in one transaction). `GET /teams/:teamID/projects`,
  `POST /teams/:teamID/projects` {name, description} → 201. Every Team-scoped read and write
  first checks membership.

Session shape in responses: `{access_token, refresh_token, access_expires_at,
refresh_expires_at, token_type: "Bearer"}`. Password rule: 8–128 characters with at least one
letter, one digit and one special character. Swagger is generated from the routes.

### Events and notification

- Customer publishes through the outbox, after commit, never inside a transaction:
  `EmailVerificationCode`, `EmailAlreadyRegistered`, `MFAEmailCode`, `PasswordReset`. Every
  payload carries the recipient's email, full name and Locale, plus the code or link.
- The customer `outbox` process relays Mongo → RabbitMQ (topic exchange, message auth on).
- The notification worker consumes each routing key, renders a `text/template` HTML email from
  `templates/notification/email/{tr,en}/` with go-i18n strings, and sends through SMTP (MailHog
  in dev) or SendGrid (other environments). A domain error is terminal; a transport error is
  retried. Notification holds no database in this phase.

### Customer gateway

- Reverse proxy per configured service host; the upstream is chosen by the required
  `X-Target-Server` header (`customer` in this phase). Missing header → 401.
- Route table in `.conf/gateway/{dev,test,alpha,prod}.yaml`: `services.<name>.{host,
  endpoints[{path, method, public, permissions, doNotLog}]}`. Unlisted → 404. `permissions` exists
  in the schema and stays empty in this phase.
- Chain: start time, correlation id, target host, public-endpoint check, JWT verification,
  session check (`session:{sid}` must exist in Redis), authorization (route lookup), proxy. Body
  audit is out of scope.
- Rate limit at the gateway is a coarse per-address limiter; the fine limits live in the service.

### Web app (`nodera-client`)

- Structure as the reference client: pages under `app/` are orchestrators; every component is a
  folder with `index.tsx` and a sibling `type.ts` exporting `<Name>Props`; API wrappers under
  `api/` with camel↔snake conversion; shared types under `core/`; every visible string in both
  `locales/tr` and `locales/en`; design-system primitives prefixed `Nodera*`.
- Palette, radii, spacing and font are CSS custom properties in one stylesheet and bridged into
  antd's theme tokens, so a brand change is one file. Light theme only; token structure ready for
  dark.
- Locale from a `NEXT_LOCALE` cookie via next-intl (default `en`), switchable in-app and sent
  along at sign-up.
- Tokens in localStorage; the axios client sends the bearer access token, adds
  `X-Target-Server`, refreshes once on 401 then retries, and clears the session if refresh fails.
  A client-side routing guard separates public and signed-in pages.
- Pages: `/login`, `/register`, `/verify-email`, `/otp`, `/forgot-password`, `/reset-password`,
  `/dashboard` (Team switcher with "new Team"; Project list of the selected Team; empty state =
  create first Project), `/dashboard/projects/new`, `/dashboard/profile` (details, password,
  MFA, sessions). The last-used Team and Project ids are kept in localStorage.
- Port 3002. Pre-commit runs format, lint, type-check, locale validation and build.

### Development environment

- docker-compose is infrastructure only: PostgreSQL 18, Redis, RabbitMQ (management UI), MongoDB
  single-node replica set (outbox only), MailHog. Services run natively via `make`.
- Ports: customer 9650, customer-gateway 3460, client 3002. Env prefixes `NODERA_CUSTOMER_`,
  `NODERA_CUSTOMER_GATEWAY_`, `NODERA_NOTIFICATION_`. `dev.toml` is gitignored; `test`, `alpha`,
  `prod` TOMLs are tracked with empty secrets.
- Dockerfiles per binary (customer, customer-gateway, notification, customer-migrate). No
  Kubernetes manifests, no CI workflows in this phase.

## Testing Decisions

A good test exercises the system through a boundary a real caller uses and asserts what that
caller can observe: the HTTP status, the error code in the envelope, the response body, the row
that now exists, the event that was published. It never asserts which internal method ran.

Seams, highest first:

1. **Customer service HTTP** (primary; the aim is for most coverage to sit here). A dockertest
   harness starts PostgreSQL, Redis, MongoDB and RabbitMQ, runs the migrations and seeds, wires
   the real Echo server exactly as the binary does, and drives it with an HTTP test client. Emails
   are observed as outbox documents / broker messages, never by reading mail. Every flow in the
   user stories gets a scenario here: sign-up (including duplicate), verification (correct, wrong,
   expired, exhausted, resend cooldown), sign-in (no MFA, MFA, unverified, suspended, lockout
   schedule and `Retry-After`), MFA verify/resend, refresh rotation and reuse revocation, sign-out
   and sign-out-everywhere, forgot/reset (single use, expiry, sessions revoked), profile updates,
   password change keeping the current session, MFA toggle requiring the password, sessions list
   and delete, Team auto-creation, Project create/list and membership refusal. Prior art: the
   `cmd/node/integration-test` suites in apex-custody (testify suite + dockertest, gated by
   `INTEGRATION=true`, exact `HttpStatus` asserted).
2. **Gateway HTTP.** The gateway wired against a stub upstream and a real Redis: unlisted route →
   404, public route passes without a token, valid token with live session is proxied with
   headers intact, valid token with deleted session → 401, missing target header → 401.
3. **Notification consumer.** Each consumer fed a real event payload with a fake mailer: correct
   template, Locale and recipient; domain error terminal, transport error retried.

Unit tests next to the code, one function per scenario with mockgen mocks (not table-driven,
except for pure matrices such as the lockout schedule, password rule, country list, token
claims). Targets: the lockout store, the token issuer/rotator, the code stores, the password rule,
the route-table matcher. Prior art: `pkg/aggregates/node/user/lockout/store_test.go` and the
handler tests in apex-custody.

The web app ships with type-check, lint and build in the pre-commit hook; no automated UI tests in
this phase.

Human verification (agreed with the user): reading a verification, MFA and reset email in MailHog
in both Locales; a browser pass through sign-up → verify → create first Project → profile → sign
out; an unlisted route returning 404 at the gateway.

## Out of Scope

- Admin service, admin gateway and admin web app.
- Droplets, Providers, regions, pricing, ordering, billing, invoices.
- Team invitations, more than one member, any Role other than `owner`, Team or Project
  renaming and deletion.
- TOTP, security keys, backup codes, step-up authentication, social sign-in.
- Gateway body audit and audit chain; Kubernetes manifests; CI workflows; observability beyond
  structured logs and a `/metrics` endpoint skeleton.
- Dark theme, tenant white-labelling, languages beyond `tr` and `en`.
- A notification database or in-app notification feed.

## Further Notes

- Identical responses on sign-up, sign-in and forgot-password regardless of whether the email
  exists are deliberate; the "already registered" email is the only channel that reveals it, and
  only to the address's owner.
- Sign-in success is a 404 with the user-not-found code for both wrong email and wrong password,
  as in the reference repos.
- The Mongo outbox is a known trade-off (ADR-0001); a PostgreSQL outbox with the same publisher
  API is the planned replacement if lost events are ever observed.
- Phase 2 candidates: Team invitations and Roles, Droplet catalogue and Provider integration,
  admin context, TOTP.
