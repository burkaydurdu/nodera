package authz_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/piggystats/nodera/pkg/authz"
)

// The matrix is the spec's table; every cell is asserted so a change to one
// Role is a visible diff here.
func TestAllows_MatchesTheSpecMatrix(t *testing.T) {
	expected := map[authz.Role]map[authz.Permission]bool{
		authz.RoleOwner: {
			authz.TeamRead: true, authz.TeamManage: true, authz.ProjectWrite: true, authz.DropletRead: true,
			authz.DropletWrite: true, authz.SSHKeyWrite: true, authz.BillingRead: true, authz.BillingWrite: true,
			authz.MailRead: true, authz.MailWrite: true, authz.MailSend: true, authz.APIKeyWrite: true,
		},
		authz.RoleDeveloper: {
			authz.TeamRead: true, authz.TeamManage: false, authz.ProjectWrite: true, authz.DropletRead: true,
			authz.DropletWrite: true, authz.SSHKeyWrite: true, authz.BillingRead: false, authz.BillingWrite: false,
			authz.MailRead: true, authz.MailWrite: true, authz.MailSend: true, authz.APIKeyWrite: true,
		},
		authz.RoleFinance: {
			authz.TeamRead: true, authz.TeamManage: false, authz.ProjectWrite: false, authz.DropletRead: true,
			authz.DropletWrite: false, authz.SSHKeyWrite: false, authz.BillingRead: true, authz.BillingWrite: true,
			authz.MailRead: true, authz.MailWrite: false, authz.MailSend: false, authz.APIKeyWrite: false,
		},
	}

	for role, perms := range expected {
		for _, permission := range authz.Permissions {
			assert.Equalf(t, perms[permission], authz.Allows(role, permission), "%s / %s", role, permission)
		}
	}
}

func TestAllows_UnknownRoleHoldsNothing(t *testing.T) {
	for _, permission := range authz.Permissions {
		assert.False(t, authz.Allows(authz.Role("admin"), permission))
		assert.False(t, authz.Allows(authz.Role(""), permission))
	}
}

func TestIsRole_KnowsOnlyTheThree(t *testing.T) {
	assert.True(t, authz.IsRole("owner"))
	assert.True(t, authz.IsRole("developer"))
	assert.True(t, authz.IsRole("finance"))
	assert.False(t, authz.IsRole("admin"))
	assert.False(t, authz.IsRole("Owner"))
}

func TestIsPermission_RejectsUnknownWords(t *testing.T) {
	assert.True(t, authz.IsPermission("team.manage"))
	assert.False(t, authz.IsPermission("team.delete"))
	assert.False(t, authz.IsPermission(""))
}

func TestIsKeyPermission_OnlySendingForNow(t *testing.T) {
	assert.True(t, authz.IsKeyPermission("mail.send"))
	assert.False(t, authz.IsKeyPermission("mail.write"))
	assert.False(t, authz.IsKeyPermission("droplet.write"))
	assert.False(t, authz.IsKeyPermission(""))
}
