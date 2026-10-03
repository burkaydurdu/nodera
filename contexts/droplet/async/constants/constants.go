package constants

const (
	CommonExchange = "CommonExchange"

	// Routing keys of the customer context's events this context consumes.
	TeamCreated           = "TeamCreated"
	TeamMemberAdded       = "TeamMemberAdded"
	TeamMemberRoleChanged = "TeamMemberRoleChanged"
	TeamMemberRemoved     = "TeamMemberRemoved"
	ProjectCreated        = "ProjectCreated"

	// Routing keys of the billing context's events this context consumes
	// (docs/adr/0006).
	TeamBillingStatusChanged      = "TeamBillingStatusChanged"
	TeamDropletsDeletionRequested = "TeamDropletsDeletionRequested"

	// Routing keys of the events this context publishes: the meter's two
	// boundaries, consumed by the billing context, and every change of a
	// Droplet's status, consumed by the notification context.
	DropletBillingStarted = "DropletBillingStarted"
	DropletBillingStopped = "DropletBillingStopped"
	DropletStatusChanged  = "DropletStatusChanged"

	// Queue names are prefixed so a second consumer of the same events never
	// competes with this one.
	QueuePrefix = "droplet."
)
