package constants

const (
	CommonExchange = "CommonExchange"

	// Routing keys of the customer context's events this context consumes.
	TeamCreated     = "TeamCreated"
	TeamMemberAdded = "TeamMemberAdded"
	ProjectCreated  = "ProjectCreated"

	// Queue names are prefixed so a second consumer of the same events never
	// competes with this one.
	QueuePrefix = "droplet."
)
