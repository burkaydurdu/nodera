package constants

const (
	CommonExchange         = "CommonExchange"
	EmailVerificationCode  = "EmailVerificationCode"
	EmailAlreadyRegistered = "EmailAlreadyRegistered"
	MFAEmailCode           = "MFAEmailCode"
	PasswordReset          = "PasswordReset"

	// Domain events other contexts build read models from (docs/adr/0004).
	TeamCreated     = "TeamCreated"
	TeamMemberAdded = "TeamMemberAdded"
	ProjectCreated  = "ProjectCreated"
)
