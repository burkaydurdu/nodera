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

	// Routing keys of the kubernetes context's requests this context consumes
	// (docs/adr/0014): a Node's Droplet asked for and given back.
	NodeRequested = "NodeRequested"
	NodeReleased  = "NodeReleased"

	// NodeRejected answers a request this context could not open a Droplet
	// for at all; once a Droplet exists, its status events tell the rest.
	NodeRejected = "NodeRejected"

	// The catalog as events, for the kubernetes context's read model: every
	// Plan and Region after each sync and on replay.
	CatalogPlanChanged   = "CatalogPlanChanged"
	CatalogRegionChanged = "CatalogRegionChanged"

	// UserDataKeyFormat is the Redis key the kubernetes context leaves a Node's
	// cloud-init under for one hour (docs/adr/0014); a wire contract between the
	// contexts, declared here rather than imported.
	UserDataKeyFormat = "node_user_data:%s"

	// Queue names are prefixed so a second consumer of the same events never
	// competes with this one.
	QueuePrefix = "droplet."
)
