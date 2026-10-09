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
	// (docs/adr/0006). The deletion request is day 21 of Dunning: it deletes
	// the Team's Clusters with its Droplets.
	TeamBillingStatusChanged      = "TeamBillingStatusChanged"
	TeamDropletsDeletionRequested = "TeamDropletsDeletionRequested"

	// Routing keys of the droplet context's events this context consumes
	// (docs/adr/0014): what happened to a Node's Droplet, a Node no Droplet
	// could be opened for, and the catalog.
	DropletStatusChanged = "DropletStatusChanged"
	NodeRejected         = "NodeRejected"
	CatalogPlanChanged   = "CatalogPlanChanged"
	CatalogRegionChanged = "CatalogRegionChanged"

	// Routing keys of the events this context publishes: the Node requests
	// the droplet context fulfils, and what notification tells people.
	NodeRequested        = "NodeRequested"
	NodeReleased         = "NodeReleased"
	ClusterStatusChanged = "ClusterStatusChanged"

	// Queue names are prefixed so a second consumer of the same events never
	// competes with this one.
	QueuePrefix = "kubernetes."

	// The realtime message type the web app refetches on (docs/adr/0007).
	RealtimeClusterChanged = "kubernetes.cluster_changed"

	// UserDataKeyFormat is the Redis key a Node's cloud-init waits under for
	// the droplet context (docs/adr/0014): a wire contract between the
	// contexts, declared here rather than imported.
	UserDataKeyFormat = "node_user_data:%s"
)
