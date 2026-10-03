package auth_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/piggystats/nodera/contexts/realtime/auth"
	"github.com/piggystats/nodera/pkg/authz"
	"github.com/piggystats/nodera/pkg/realtime"
)

func TestIdentity_Accepts(t *testing.T) {
	userID, teamID, otherTeam := uuid.New(), uuid.New(), uuid.New()
	identity := &auth.Identity{UserID: userID, Teams: map[uuid.UUID]authz.Role{teamID: authz.RoleDeveloper}}
	cases := []struct {
		name    string
		address realtime.Address
		accepts bool
	}{
		{"their own User", realtime.ToUser(userID), true},
		{"another User", realtime.ToUser(uuid.New()), false},
		{"their Team, a Permission their Role holds", realtime.ToTeam(teamID, authz.DropletRead), true},
		{"their Team, a Permission their Role lacks", realtime.ToTeam(teamID, authz.BillingRead), false},
		{"a Team they are not in", realtime.ToTeam(otherTeam, authz.DropletRead), false},
		{"nobody", realtime.Address{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.accepts, identity.Accepts(c.address))
		})
	}
}
