// Package authz is the Role → Permission matrix every Nodera service and the
// customer gateway share (docs/adr/0005). It is deliberately code, not data:
// a Team cannot define its own Roles, and a reader can audit who may do what
// without a database. The package depends on nothing so that any context or
// the gateway may import it without coupling to another.
package authz

// Role is a named bundle of Permissions a member holds in one Team.
type Role string

// Permission is one thing a Role allows, named <area>.<verb>.
type Permission string

const (
	RoleOwner     Role = "owner"
	RoleDeveloper Role = "developer"
	RoleFinance   Role = "finance"
)

const (
	TeamRead     Permission = "team.read"
	TeamManage   Permission = "team.manage"
	ProjectWrite Permission = "project.write"
	DropletRead  Permission = "droplet.read"
	DropletWrite Permission = "droplet.write"
	SSHKeyWrite  Permission = "sshkey.write"
	BillingRead  Permission = "billing.read"
	BillingWrite Permission = "billing.write"
)

// Roles lists every Role in a stable order, for enums, validators and UIs.
var Roles = []Role{RoleOwner, RoleDeveloper, RoleFinance}

// Permissions lists every Permission in a stable order, for config validation.
var Permissions = []Permission{
	TeamRead, TeamManage, ProjectWrite, DropletRead, DropletWrite, SSHKeyWrite, BillingRead, BillingWrite,
}

var matrix = map[Role]map[Permission]struct{}{
	RoleOwner:     set(Permissions...),
	RoleDeveloper: set(TeamRead, ProjectWrite, DropletRead, DropletWrite, SSHKeyWrite),
	RoleFinance:   set(TeamRead, DropletRead, BillingRead, BillingWrite),
}

// Allows reports whether the Role holds the Permission. An unknown Role holds
// nothing, so a stale or mistyped value fails closed.
func Allows(role Role, permission Permission) bool {
	_, ok := matrix[role][permission]

	return ok
}

// IsRole reports whether the value names a Role this version knows.
func IsRole(value string) bool {
	_, ok := matrix[Role(value)]

	return ok
}

// IsPermission reports whether the value names a Permission this version knows,
// so a route table cannot guard a route with a word nobody holds.
func IsPermission(value string) bool {
	for _, p := range Permissions {
		if string(p) == value {
			return true
		}
	}

	return false
}

func set(permissions ...Permission) map[Permission]struct{} {
	out := make(map[Permission]struct{}, len(permissions))
	for _, p := range permissions {
		out[p] = struct{}{}
	}

	return out
}
