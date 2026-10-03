package constants

const (
	CommonExchange         = "CommonExchange"
	EmailVerificationCode  = "EmailVerificationCode"
	EmailAlreadyRegistered = "EmailAlreadyRegistered"
	MFAEmailCode           = "MFAEmailCode"
	PasswordReset          = "PasswordReset"
	TeamInvitation         = "TeamInvitation"

	// Domain events other contexts build read models from (docs/adr/0004).
	TeamCreated           = "TeamCreated"
	TeamMemberAdded       = "TeamMemberAdded"
	TeamMemberRoleChanged = "TeamMemberRoleChanged"
	TeamMemberRemoved     = "TeamMemberRemoved"
	// UserProfileUpdated tells the read models a person's name and Locale changed.
	UserProfileUpdated = "UserProfileUpdated"
	ProjectCreated     = "ProjectCreated"
)
