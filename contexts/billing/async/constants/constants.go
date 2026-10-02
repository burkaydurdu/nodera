package constants

const (
	CommonExchange = "CommonExchange"

	// Routing keys of the customer context's events this context consumes.
	TeamCreated           = "TeamCreated"
	TeamMemberAdded       = "TeamMemberAdded"
	TeamMemberRoleChanged = "TeamMemberRoleChanged"
	TeamMemberRemoved     = "TeamMemberRemoved"

	// Routing keys of the droplet context's usage events (consumed by the
	// usage aggregate).
	DropletBillingStarted = "DropletBillingStarted"
	DropletBillingStopped = "DropletBillingStopped"

	// Routing keys of the events this context publishes.
	TeamBillingStatusChanged      = "TeamBillingStatusChanged"
	TeamDropletsDeletionRequested = "TeamDropletsDeletionRequested"
	InvoiceIssued                 = "InvoiceIssued"
	PaymentSucceeded              = "PaymentSucceeded"
	PaymentFailed                 = "PaymentFailed"
	PaymentConfirmationRequired   = "PaymentConfirmationRequired"
	TeamSuspended                 = "TeamSuspended"
	DropletsDeletionWarning       = "DropletsDeletionWarning"
	CardExpiring                  = "CardExpiring"

	// Queue names are prefixed so a second consumer of the same events never
	// competes with this one.
	QueuePrefix = "billing."
)
