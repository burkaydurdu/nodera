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
What a member may do inside one Team. A member holds exactly one Role per Team. Today only
`owner`, the Role of the User who created the Team.
_Avoid_: permission (a Role is made of them), group

**Project**:
A named grouping of a Team's resources, like a namespace. Belongs to exactly one Team. A User
creates their first one right after signing in; Droplets will live inside it.
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
