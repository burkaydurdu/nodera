package cherry

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/piggystats/nodera/contexts/droplet/providers"
)

func TestMapStatus(t *testing.T) {
	cases := []struct {
		name                 string
		state, status, power string
		expect               providers.Status
	}{
		{"deployed and active", "active", "deployed", "on", providers.StatusActive},
		{"deployed, power unknown", "active", "deployed", "", providers.StatusActive},
		{"deployed but powered off", "active", "deployed", "off", providers.StatusOff},
		{"allocated counts as delivered", "active", "allocated", "", providers.StatusActive},
		{"still deploying", "pending", "deploying", "", providers.StatusProvisioning},
		{"failed deployment", "active", "failed deployment", "", providers.StatusFailed},
		{"terminating", "terminating", "deployed", "", providers.StatusDeleting},
		{"terminated", "terminated", "", "", providers.StatusDeleted},
		{"unknown strings are provisioning", "weird", "stranger", "", providers.StatusProvisioning},
		{"case and spaces do not matter", " Active ", "DEPLOYED", "OFF", providers.StatusOff},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expect, MapStatus(tc.state, tc.status, tc.power))
		})
	}
}
