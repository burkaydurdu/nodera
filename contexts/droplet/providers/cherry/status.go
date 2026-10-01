package cherry

import (
	"strings"

	"github.com/piggystats/nodera/contexts/droplet/providers"
)

const (
	powerOff = "off"

	statusDeployed  = "deployed"
	statusAllocated = "allocated"
	statusFailed    = "failed deployment"
)

// MapStatus is the one table from Cherry's server `state` and `status`
// strings (plus the power state when known) to a Droplet status. Anything
// unrecognised is still on its way: provisioning.
func MapStatus(state, status, power string) providers.Status {
	state = strings.ToLower(strings.TrimSpace(state))
	status = strings.ToLower(strings.TrimSpace(status))
	power = strings.ToLower(strings.TrimSpace(power))

	switch {
	case status == statusFailed, state == "failed", state == "error":
		return providers.StatusFailed
	case state == "terminating", state == "deleting", status == "terminating", status == "deleting":
		return providers.StatusDeleting
	case state == "terminated", state == "deleted", status == "terminated":
		return providers.StatusDeleted
	case status == statusDeployed, status == statusAllocated:
		if state == "off" || state == "stopped" || state == "inactive" || power == powerOff {
			return providers.StatusOff
		}

		return providers.StatusActive
	case state == "off", state == "stopped":
		return providers.StatusOff
	default:
		return providers.StatusProvisioning
	}
}
