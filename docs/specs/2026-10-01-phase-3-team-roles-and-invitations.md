# Phase 3: Team Roles, Permissions and Invitations

Vocabulary: `contexts/customer/CONTEXT.md` (Role, Permission, Invitation, Team, Session) and
`contexts/droplet/CONTEXT.md` (Project access). Decisions already recorded: ADR-0002
(sessions), ADR-0004 (the droplet context learns Projects and members from events), ADR-0005
(Team Roles ride the access token, with a permission version).

## Problem Statement

A Team today is one person. The User who signs up gets a Team with the single Role `owner`,
and nobody else can join it: there is no way to invite a colleague, no way to give an
accountant access to invoices without also letting them delete servers, and no way for a
developer to work in a Team without owning it. Every real customer is a company or a group,
and the product cannot yet hold one.

## Solution

Teams become multi-member with three Roles and an Invitation flow:

- **Roles** `owner`, `developer`, `finance`, each a fixed bundle of Permissions. Owners run the
  Team and its members; developers work with Projects, Droplets and SSH keys; finance sees
  Droplets and, in the next phase, handles invoices and payment methods. A Team may have
  several owners.
- **Invitations**: an owner invites an email address with a Role. The invitee gets an email
  with an accept link, sees pending Invitations inside the app, and accepts or declines. A
  person without an account signs up with that email and the Invitation completes itself.
  Owners can resend and revoke; Invitations expire after seven days.
- **Member management**: owners change Roles and remove members; anyone can leave; the last
  owner is protected.
- **Enforcement in two gates**: the access token carries the caller's Role per Team and a
  permission version; the customer gateway refuses a request whose Role lacks the route's
  Permission before it reaches a service, and every service enforces the same rule itself. A
  Role change takes effect within seconds through a silent token refresh, never a sign-out.
- **Web app**: Members and Invitations tabs under the Team, an invite dialog, an accept page,
  a pending-invitation badge, and menus that shrink to what the Role allows.

## User Stories

### Roles and what they allow

1. As an owner, I want my Team to have the Roles owner, developer and finance, so that I can
   give each colleague exactly the access their job needs.
2. As an owner, I want more than one owner to be possible, so that the Team does not depend on
   one person being available.
3. As a developer, I want to create Projects, Droplets and SSH keys in my Team, so that I can
   do my work without owning the Team.
4. As a developer, I want to be kept out of invoices and payment methods, so that I cannot see
   or change what is not my job.
5. As a finance member, I want to see the Team's Droplets and their prices, so that I can
   understand the invoice lines.
6. As a finance member, I want to be unable to create, power or delete Droplets, so that I
   cannot cause costs by accident.
7. As any member, I want to see the Team's members and their Roles, so that I know who can do
   what.
8. As a member without a Permission, I want a clear "not allowed" answer rather than "not
   found", so that I know the Team exists and whom to ask.
9. As a non-member, I want a Team I do not belong to answer "not found", so that Team ids tell
   outsiders nothing.
10. As Nodera, I want the Role-to-Permission matrix fixed in code and identical for every
    Team, so that authorization is auditable and never configured per Team.

### Invitations

11. As an owner, I want to invite an email address to my Team with a Role, so that a colleague
    can join with the right access from the start.
12. As an owner, I want to invite someone as owner, developer or finance, so that a co-founder
    can join as an equal.
13. As an owner, I want the invitee to receive an email naming me, the Team and the Role, with
    an accept link, so that they trust the invitation and know what they are accepting.
14. As an owner, I want to be told if the address is already a member or already has a
    pending Invitation, so that I do not send duplicates.
15. As an owner, I want to see pending Invitations with who invited, the Role, when it expires
    and its state, so that I can follow up.
16. As an owner, I want to resend an Invitation, so that a lost email is not a dead end.
17. As an owner, I want to revoke an Invitation, so that a mistaken invite stops working.
18. As an owner, I want Invitations to expire after seven days, so that forgotten invites do
    not stay open.
19. As an owner, I want declined Invitations to show as declined, so that I know the answer.
20. As an invitee with an account, I want to click the email link while signed in and join
    with one confirmation, so that joining takes seconds.
21. As an invitee signed in with a different email, I want to be told the Invitation belongs
    to another address, so that I switch accounts instead of wondering why it fails.
22. As an invitee without an account, I want the link to take me to sign-up with the email
    already filled and locked, so that I cannot register the wrong address.
23. As an invitee who just signed up from an Invitation, I want the Invitation accepted
    automatically after email verification, so that I land in the Team without a second step.
24. As an invitee signed out, I want the link to take me to sign-in and then complete the
    Invitation, so that I do not have to find the email again.
25. As a User, I want to see pending Invitations to my email inside the app, so that I can
    accept or decline even without the email.
26. As a User, I want to decline an Invitation, so that I am not added to a Team I do not
    want.
27. As a User, I want an expired or revoked link to say so plainly, so that I ask for a new
    one.
28. As a User who accepted an Invitation, I want the new Team selected in the app right away,
    so that I see what I just joined.
29. As Nodera, I want Invitation tokens to be unguessable and stored only as a hash, so that a
    database leak does not hand out memberships.
30. As Nodera, I want Invitations rate-limited per Team and per inviter, so that the feature
    cannot be used to spam addresses.

### Member management

31. As an owner, I want to change a member's Role, so that access follows responsibilities.
32. As an owner, I want to remove a member, so that someone who left the company loses access.
33. As any member, I want to leave a Team, so that I am not stuck in Teams I no longer work
    in.
34. As Nodera, I want the last owner to be impossible to remove, demote or let leave, so that
    no Team is ever ownerless.
35. As an owner, I want to be unable to change my own Role, so that a Team never loses an
    owner by a misclick; another owner must do it.
36. As a removed member, I want my access to the Team and its Droplets to end within seconds,
    so that removal means removal.
37. As a member whose Role changed, I want the new Permissions to apply without signing out,
    so that nothing I am doing is interrupted.
38. As a member, I want the Team switcher to show my Role in each Team, so that I know what I
    can do where.

### Web app

39. As an owner, I want a Team settings area with Members and Invitations tabs, so that
    managing the Team is one place.
40. As an owner, I want an invite dialog with email and Role, so that inviting is one step.
41. As a member, I want the side menu and buttons to show only what my Role allows, so that I
    am not offered actions that will be refused.
42. As a User with pending Invitations, I want a badge in the navbar, so that I notice them.
43. As a User, I want an accept page that works from the email link in every state (signed
    in, other account, signed out, no account, expired), so that the link never dead-ends.
44. As a User, I want all of this in Turkish and English, so that the Team reads it in its
    Locale.

### Operating Nodera

45. As Nodera, I want the gateway to refuse a request whose Role lacks the route's Permission,
    so that services are not the only gate.
46. As Nodera, I want services to enforce Permissions themselves as well, so that a gateway
    misconfiguration cannot open a route.
47. As Nodera, I want the droplet service to learn Role changes and removals through events,
    so that it enforces Droplet Permissions without asking the customer service.
48. As Nodera, I want access tokens issued before this change to be refreshed silently, so
    that the deployment signs nobody out.

## Implementation Decisions

### Roles and Permissions

- Roles: `owner`, `developer`, `finance`. The membership column's enum grows accordingly;
  existing memberships stay `owner`.
- Permissions, named `<area>.<verb>`: `team.read`, `team.manage`, `project.write`,
  `droplet.read`, `droplet.write`, `sshkey.write`, `billing.read`, `billing.write`. The
  `billing.*` Permissions exist now so the finance Role is complete; no billing route uses
  them until the billing phase.
- The matrix lives in one shared, dependency-free package (`pkg/authz`) imported by the
  gateway, the customer service and the droplet service: owner has everything; developer has
  `team.read`, `project.write`, `droplet.read`, `droplet.write`, `sshkey.write`; finance has
  `team.read`, `droplet.read`, `billing.read`, `billing.write`. The package exposes
  `Allows(role, permission)` and the list of Roles; nothing else.
- Answers: non-member → the existing "not a member" 404; member without the Permission → a
  new 403 (`team` domain, "not allowed for your role"). The droplet service answers 404 for
  unknown Project / non-member (unchanged) and 403 for a member whose Role lacks
  `droplet.write`.

### Access token and freshness (ADR-0005)

- Access claims gain `teams` (map of Team id → Role) and `pv` (the User's permission version,
  an integer). Issuing an access token, at sign-in and at refresh, reads memberships from the
  database and the version from Redis (`user_perm_version:{userID}`, default 0).
- The customer service increments the version whenever a membership of that User changes:
  Invitation accepted, Role changed, removed, left, Team created.
- The gateway, after the session check, compares the token's `pv` with Redis; a mismatch or a
  token without the claim answers 401 so the web app's existing refresh-once path mints a new
  token. The two Redis reads are cheap point reads; pipelining them is an optimisation left
  for when gateway latency asks for it.
- Refresh keeps rotating as today; its handler is where new Roles arrive in a token.

### Gateway

- Route entries may carry `permissions`. For a route whose path has a `:teamID` parameter the
  gateway reads the Team id from the request path, looks up the Role in `teams`, and requires
  every listed Permission; no Role for that Team → 404, Role without the Permission → 403.
- Routes without `:teamID` never carry `permissions` in this phase; the gateway refuses to
  start if the YAML lists Permissions on such a route, so a misconfiguration is loud.
- The Team routes get their Permissions in all four YAMLs: projects and SSH keys writes →
  `project.write` / `sshkey.write`; members and Invitations management → `team.manage`;
  reads → `team.read`.

### Invitations (customer context, aggregate `invitation`)

- Fields: id, Team, email (lower-cased, trimmed), Role, invited-by, token hash (SHA-256 of a
  32-byte random token), expires-at (7 days), status (`pending`, `accepted`, `declined`,
  `revoked`, `expired`), accepted-by, responded-at, created-at. Unique: one `pending` per
  (Team, email).
- Create (owner, `team.manage`): email already a member → 409; pending Invitation exists →
  409; otherwise insert, then after commit publish the `team_invitation` email event with the
  inviter's name, Team name, Role and the accept URL (`{front}/invitations/accept?token=`).
- Resend (owner): new token and expiry, 60-second cooldown per Invitation, same email event.
- Revoke (owner): status `revoked`; the token stops working.
- List for a Team (owner): all statuses, newest first. Members without `team.manage` get 403.
- List mine (any User): pending Invitations whose email equals the User's verified email,
  with Team name, inviter name and Role.
- Accept by token (signed in): token hash lookup; not pending or expired → 410 "no longer
  valid"; email differs from the caller's → 403 "invitation is for another email"; already a
  member → mark accepted and answer success anyway; otherwise, in one transaction, add the
  membership with the Role and mark accepted, then bump the permission version and publish
  `TeamMemberAdded`. The response carries the Team so the web app can select it.
- Accept by id (from the in-app list) and decline by id follow the same rules; decline marks
  `declined`.
- Sign-up with an Invitation: the sign-up request may carry `invitation_token`; the token's
  email must equal the sign-up email (else 400); the pending Invitation id is remembered on
  the User. When email verification completes, the Invitation is accepted in the same flow and
  the sign-in response includes the Team. A sign-up from an Invitation does not get the usual
  auto-created Team suppressed: the User still gets their own Team, plus the invited one.
- Expiry is lazy: a pending Invitation past its date behaves as `expired` wherever it is read
  and is shown as such.
- Rate limits: create 20 per Team per hour and 50 per inviter per day; accept/decline by token
  20 per IP per 10 minutes; resend cooldown 60 seconds per Invitation.

### Members (customer context, `team` aggregate)

- Routes: list members (`team.read`) with User name, email, Role, joined-at; change Role
  (`team.manage`); remove member (`team.manage`); leave (any member, on self).
- Rules: a member cannot change their own Role (409); the last owner cannot be demoted,
  removed or leave (409 "last owner"); removing or demoting bumps the target's permission
  version and publishes `TeamMemberRemoved` or `TeamMemberRoleChanged`.
- Team list for a User continues to return the Role per Team.

### Events and the droplet context

- Customer publishes `TeamMemberRoleChanged` (team id, user id, role, changed-at) and
  `TeamMemberRemoved` (team id, user id, removed-at) in addition to the existing three.
- The droplet worker consumes them into `team_access` (update Role / delete row). Droplet
  routes check `droplet.read` for reads and `droplet.write` for create, actions and delete,
  using the shared matrix; a member whose Role lacks it gets 403.
- `replay-projects` republishes memberships with their current Role, as today.

### Notification

- New email kind `team_invitation` (tr/en templates): inviter name, Team name, Role, accept
  link, expiry date.

### Web app

- Team settings (`/dashboard/team`): **Members** tab (table with name, email, Role, joined;
  owner sees a Role select and Remove; everyone sees Leave on their own row) and
  **Invitations** tab (owner only: pending and past Invitations, Resend, Revoke, and an
  "Invite" dialog with email and Role).
- Accept page (`/invitations/accept?token=`): resolves the Invitation (Team, Role, inviter)
  through a token-scoped public read, then: signed in with matching email → Accept / Decline;
  signed in with another email → explanation and a "switch account" action; signed out →
  Sign in / Sign up buttons that carry the token; sign-up prefills and locks the email; after
  verification the app accepts and selects the Team. Expired or revoked → a plain message.
- Navbar badge with the count of pending Invitations (from the "mine" endpoint, fetched on
  load and after sign-in); a small list to accept or decline inline.
- Role-aware UI: the side menu and action buttons consult the current Team's Role through the
  same matrix mirrored in the client (`core/authz`), hiding Create Droplet, SSH keys and
  Project creation for finance and the Members management controls for non-owners. The server
  remains the authority; the client only hides.
- The 401 refresh path is unchanged and now also delivers Role changes.
- All strings in both locale files.

## Testing Decisions

A good test drives a public interface the way a client would and asserts what the client
sees: HTTP status, body, an outbox document, a row the next request returns. It never
inspects handlers or repositories.

Seams, highest first:

1. **Customer integration suite** (existing, primary): Role matrix in effect on Team routes
   (developer creates a Project, finance cannot: 403; non-member 404); Invitations end to end
   (create → outbox email document with the accept URL → accept by token as the invitee →
   member with the Role, `TeamMemberAdded` document, permission version bumped → token refresh
   carries the new Team); duplicates 409; expired/revoked 410; wrong email 403; decline; resend
   cooldown; sign-up with `invitation_token` followed by verification joins the Team; Role
   change, remove, leave; last-owner protections; self-Role change refused; access claims
   contain `teams` and `pv`; rate limits.
2. **Gateway integration suite** (existing): a route with `permissions` admits an owner and
   refuses a finance member with 403 and a non-member with 404; a stale `pv` answers 401; a
   token without the claims answers 401; the gateway refuses to start when a non-Team route
   lists Permissions.
3. **Droplet integration suite** (existing): after consuming `TeamMemberAdded` with
   `finance`, the member lists Droplets but cannot create (403); after `TeamMemberRoleChanged`
   to developer the same call succeeds; after `TeamMemberRemoved` it is 404.
4. **Unit**: the matrix (table-driven: every Role × every Permission), the token-hash and
   expiry helpers, the notification consumer for `team_invitation`.

Prior art: `cmd/customer/integration-test` (outbox assertions, register/verify helpers),
`cmd/customer-gateway/gateway_integration_test.go`, `cmd/droplet/integration-test`.

## Out of Scope

- Billing routes, invoices, payment methods, Craftgate and Stripe (next phase; this phase
  only reserves the `billing.*` Permissions and the finance Role).
- Team deletion, ownership "transfer" as a distinct action (promote another owner, then
  leave), renaming a Team, Team avatars.
- Custom Roles or per-Team Permission editing.
- Changing a User's email address.
- Per-Project access within a Team.
- Audit log of membership changes (events are published; nothing stores them yet).
- Admin/operator tooling.

## Further Notes

- "admin" is not a Role in Nodera; the word is reserved for the operator side.
- The `finance` Role already has `billing.*` so that, when invoices arrive, no re-grant is
  needed; until then those Permissions guard nothing.
- The gateway's `permissions` YAML field, present since Phase 1, is used for the first time.
