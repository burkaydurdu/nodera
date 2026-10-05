# Customer

The customer-facing context. It owns the people who sign up to Nodera, how they prove who they
are, and the teams and projects their resources will be grouped under. "Customer" names the
context and its service, never a record: the people are Users, the groups are Teams.

## Language

### Product

**Droplet**:
What a customer buys: a bare-metal server matching the CPU/RAM/disk/region the customer asked for,
rented from a Provider and set up by the platform. The customer sees only the Droplet.
_Avoid_: server, instance, VM, machine

**Provider**:
An integrated bare-metal supplier the platform rents physical servers from through its API. Never
visible to a customer.
_Avoid_: vendor, supplier, upstream, datacenter

**Mail**:
Nodera's second product: a Team's software sends email from the Team's own domains, and Nodera
delivers it from its own mail servers. Lives in a Project. Defined in the mail context as well.
_Avoid_: email (the notification Channel), mail provider

**App**:
One product's own area of the web app, with its own side menu, that a User switches to: Droplets
or Mail. An arrangement of the web app; Nodera keeps no record of it.
_Avoid_: module, product page, service (a service is a backend binary)

### People and groups

**User**:
A person who signs in to Nodera with an email address and password. A User can use the platform
alone or as a member of Teams.
_Avoid_: customer, member (that is a User seen from inside a Team), account, login

**Team**:
A group of Users working on the same resources. Every User gets one at sign-up and may create
more; each member holds a Role in it. Nothing is owned outside a Team.
_Avoid_: workspace, organisation, customer, tenant, personal account

**Role**:
What a member may do inside one Team, as a named bundle of Permissions. A member holds exactly
one Role per Team. `owner` runs the Team and its members (the creator starts as one; a Team may
have several), `developer` works with Projects, Droplets, SSH keys, Mail and API keys, `finance`
sees Droplets and Mail and handles invoices and payment methods.
_Avoid_: admin (that is Nodera's own operator side), permission (a Role is made of them), group

**Permission**:
One thing a Role or an API key allows, named `<area>.<verb>` such as `team.manage` or
`droplet.write`. The mapping from Role to Permissions is fixed by Nodera, the same for every Team,
and never edited per Team.
_Avoid_: right, scope, grant, capability

**Invitation**:
An owner's pending offer of a Role in a Team to an email address, whether or not a User exists
for it yet. It is accepted once by the User who proves that email is theirs, or declined by
them, or revoked by an owner, or it expires.
_Avoid_: invite (the verb), request (that would come from the joiner), share

**Project**:
A named grouping of a Team's resources, like a namespace. Belongs to exactly one Team. A User
creates their first one right after signing in; Droplets and Mail live inside it.
_Avoid_: workspace, folder, environment

**SSH key**:
A public key a Team keeps so that Droplets can be created with it installed. Belongs to a Team,
has a name and a fingerprint, and is never a secret.
_Avoid_: key pair (Nodera never holds the private half), credential

**Locale**:
The language a User reads Nodera and its emails in, one of `tr` or `en`. Chosen by the User and
changed in profile settings.
_Avoid_: language, lang

**Country**:
The country a User declares for their account. Chosen by the User and changed in profile
settings.
_Avoid_: region (that is where a Droplet runs), location

### Identity

**Email verification**:
The step after sign-up where the User proves the email address is theirs by entering the code
sent to it. A User cannot sign in until it is done.
_Avoid_: activation, confirmation, MFA (that is a sign-in step)

**MFA method**:
The second factor a User presents at sign-in, chosen per User. Today only `email`. On by default;
a User may turn it off.
_Avoid_: 2FA, OTP (that is the code, not the method), factor

**Login challenge**:
The short-lived pending sign-in that exists between a correct password and a correct MFA code. It
is what the MFA code answers.
_Avoid_: pre-auth token, session (that comes after), OTP request

**Session**:
A signed-in User on one device, represented by an access token and the refresh token that renews
it. A User may hold several.
_Avoid_: token (bare), login

**API key**:
A secret a Team's software presents to call Nodera through the integration gateway instead of a
User signing in (ADR-0010). Belongs to one Project, acts only on it, and carries Permissions chosen
when it is created, never wider than its creator's.
_Avoid_: token (bare; that is a Session's), access key, credential, secret
