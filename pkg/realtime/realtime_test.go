package realtime_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/piggystats/nodera/pkg/authz"
	"github.com/piggystats/nodera/pkg/realtime"
)

func TestAddress_Valid(t *testing.T) {
	id := uuid.New()
	nilID := uuid.Nil
	cases := []struct {
		name    string
		address realtime.Address
		valid   bool
	}{
		{"a User", realtime.ToUser(id), true},
		{"a Team with a Permission", realtime.ToTeam(id, authz.DropletRead), true},
		{"a Team without a Permission", realtime.Address{TeamID: &id}, false},
		{"a Team with an unknown Permission", realtime.ToTeam(id, "droplet.everything"), false},
		{"both a User and a Team", realtime.Address{UserID: &id, TeamID: &id, Permission: authz.DropletRead}, false},
		{"nobody", realtime.Address{}, false},
		{"the nil User", realtime.Address{UserID: &nilID}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.valid, c.address.Valid())
		})
	}
}
