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
	// the Team's Storages. It keeps the name it had when only Droplets were
	// deleted that day.
	TeamBillingStatusChanged      = "TeamBillingStatusChanged"
	TeamDropletsDeletionRequested = "TeamDropletsDeletionRequested"

	// Routing keys of the events this context publishes: the Package
	// intervals billing charges, and what notification tells people.
	StoragePackageStarted   = "StoragePackageStarted"
	StoragePackageStopped   = "StoragePackageStopped"
	StorageNearlyFull       = "StorageNearlyFull"
	StorageFull             = "StorageFull"
	StoragePaused           = "StoragePaused"
	StorageDowngradeSkipped = "StorageDowngradeSkipped"

	// Queue names are prefixed so a second consumer of the same events never
	// competes with this one.
	QueuePrefix = "storage."

	// The realtime message type the web app refetches on (docs/adr/0007).
	RealtimeStorageChanged = "storage.changed"
)
