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

	// QueuePrefix keeps this context's queues for the shared domain events
	// apart from the droplet and billing contexts' queues of the same events.
	QueuePrefix = "notification."
)
