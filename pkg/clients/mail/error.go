package mail

import "github.com/piggystats/rerror"

const Domain rerror.Domain = 9

var (
	ErrFailedToBuildMessage = rerror.Error{Code: 10001, Domain: Domain, Message: "failed to build email message"}
	// ErrFailedToSendEmail is retryable: the provider may be back on the next
	// delivery, and a dropped verification code costs the person a resend.
	ErrFailedToSendEmail = rerror.Error{Code: 10002, Domain: Domain, Message: "failed to send email", IsRetryable: true}
)
