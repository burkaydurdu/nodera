package handlers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/piggystats/nodera/contexts/customer/aggregates/apikey"
	"github.com/piggystats/nodera/contexts/customer/aggregates/team"
	"github.com/piggystats/nodera/pkg/authz"
)

// No Role that may make keys lacks mail.send today, so the ceiling is only
// reachable here; it must hold the day a key may carry more.
func TestKeyPermissions_APermissionTheRoleLacksIsForbidden(t *testing.T) {
	_, err := keyPermissions(team.RoleFinance, []string{"mail.send"})

	assert.ErrorIs(t, err, apikey.ErrorPermissionNotHeld)
}

func TestKeyPermissions_WhatNoKeyMayCarryIsRefusedBeforeTheRole(t *testing.T) {
	_, err := keyPermissions(team.RoleFinance, []string{"mail.send", "droplet.write"})

	assert.ErrorIs(t, err, apikey.ErrorPermissionNotAllowed)
}

func TestKeyPermissions_WhatTheRoleHoldsIsGranted(t *testing.T) {
	permissions, err := keyPermissions(team.RoleDeveloper, []string{"mail.send"})

	require.NoError(t, err)
	assert.Equal(t, []authz.Permission{authz.MailSend}, permissions)
}
