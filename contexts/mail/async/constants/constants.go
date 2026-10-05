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
	// (docs/adr/0006). The deletion request is day 21 of Dunning: it ends the
	// Team's Packages.
	TeamBillingStatusChanged      = "TeamBillingStatusChanged"
	TeamDropletsDeletionRequested = "TeamDropletsDeletionRequested"

	// Routing keys of the events this context publishes: the Package
	// intervals billing charges, and what notification tells people.
	MailPackageStarted   = "MailPackageStarted"
	MailPackageStopped   = "MailPackageStopped"
	MailDomainVerified   = "MailDomainVerified"
	MailDomainUnverified = "MailDomainUnverified"
	MailPackageNearLimit = "MailPackageNearLimit"
	MailPackageExhausted = "MailPackageExhausted"
	MailRatesWarning     = "MailRatesWarning"
	MailSendingPaused    = "MailSendingPaused"

	// Queue names are prefixed so a second consumer of the same events never
	// competes with this one.
	QueuePrefix = "mail."

	// Realtime message types the web app refetches on (docs/adr/0007).
	RealtimeDomainChanged  = "mail.domain_changed"
	RealtimeSendingChanged = "mail.sending_changed"
)
