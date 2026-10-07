package constants

const (
	CommonExchange         = "CommonExchange"
	EmailVerificationCode  = "EmailVerificationCode"
	EmailAlreadyRegistered = "EmailAlreadyRegistered"
	MFAEmailCode           = "MFAEmailCode"
	PasswordReset          = "PasswordReset"
	TeamInvitation         = "TeamInvitation"

	InvoiceIssued               = "InvoiceIssued"
	PaymentSucceeded            = "PaymentSucceeded"
	PaymentFailed               = "PaymentFailed"
	PaymentConfirmationRequired = "PaymentConfirmationRequired"
	TeamSuspended               = "TeamSuspended"
	DropletsDeletionWarning     = "DropletsDeletionWarning"
	CardExpiring                = "CardExpiring"

	// The customer context's Team events, for the membership read model.
	TeamCreated           = "TeamCreated"
	TeamMemberAdded       = "TeamMemberAdded"
	TeamMemberRoleChanged = "TeamMemberRoleChanged"
	TeamMemberRemoved     = "TeamMemberRemoved"
	UserProfileUpdated    = "UserProfileUpdated"

	// The droplet context's status changes.
	DropletStatusChanged = "DropletStatusChanged"

	// The mail context's news about a Project's Mail.
	MailDomainVerified   = "MailDomainVerified"
	MailDomainUnverified = "MailDomainUnverified"
	MailPackageNearLimit = "MailPackageNearLimit"
	MailPackageExhausted = "MailPackageExhausted"
	MailRatesWarning     = "MailRatesWarning"
	MailSendingPaused    = "MailSendingPaused"

	// The storage context's news about a Storage.
	StorageNearlyFull       = "StorageNearlyFull"
	StorageFull             = "StorageFull"
	StoragePaused           = "StoragePaused"
	StorageDowngradeSkipped = "StorageDowngradeSkipped"

	// QueuePrefix keeps this context's queues for the shared domain events
	// apart from the droplet and billing contexts' queues of the same events.
	QueuePrefix = "notification."
)
