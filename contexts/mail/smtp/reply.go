package smtp

import (
	"errors"

	gosmtp "github.com/emersion/go-smtp"
	"github.com/piggystats/rerror"

	"github.com/piggystats/nodera/contexts/mail/aggregates/billing"
	"github.com/piggystats/nodera/contexts/mail/aggregates/limits"
	"github.com/piggystats/nodera/contexts/mail/aggregates/message"
	"github.com/piggystats/nodera/contexts/mail/aggregates/packages"
	"github.com/piggystats/nodera/contexts/mail/apikey"
)

type reply struct {
	match    rerror.Error
	code     int
	enhanced gosmtp.EnhancedCode
	text     string
}

// replies maps the send path's refusals to SMTP: a temporary state (the
// Daily limit, the allowance, a Pause, a suspension) is 4xx so the client's
// MTA keeps the Message and retries; what only the Team can fix is 5xx.
var replies = []reply{
	{billing.ErrorTeamSuspended, 451, gosmtp.EnhancedCode{4, 7, 1}, "Sending is stopped while the Team is suspended"},
	{limits.ErrorPaused, 451, gosmtp.EnhancedCode{4, 7, 1}, "Sending is paused for this project"},
	{limits.ErrorDailyLimitReached, 452, gosmtp.EnhancedCode{4, 5, 3}, "The daily limit is reached"},
	{packages.ErrorPackageExhausted, 452, gosmtp.EnhancedCode{4, 5, 3}, "The monthly allowance is used up"},
	{packages.ErrorNotEnoughAllowance, 452, gosmtp.EnhancedCode{4, 5, 3}, "The monthly allowance has fewer emails left than the message has recipients"},
	{packages.ErrorNoPackage, 550, gosmtp.EnhancedCode{5, 7, 1}, "The project has no mail package"},
	{message.ErrorSenderNotVerified, 550, gosmtp.EnhancedCode{5, 7, 1}, "The sender is not on a verified sending domain"},
	{message.ErrorMalformedMessage, 554, gosmtp.EnhancedCode{5, 6, 0}, "The message could not be read"},
	{message.ErrorTooManyRecipients, 550, gosmtp.EnhancedCode{5, 5, 3}, "At most 50 recipients"},
	{message.ErrorNoRecipients, 554, gosmtp.EnhancedCode{5, 5, 1}, "No valid recipients"},
	{message.ErrorInvalidAddress, 553, gosmtp.EnhancedCode{5, 1, 3}, "Not an email address"},
	{apikey.ErrorUnavailable, 454, gosmtp.EnhancedCode{4, 7, 0}, "Temporary authentication failure"},
}

// Reply is the SMTP answer for an error of the send path, and whether the
// error is a refusal of the send path's; anything else is Nodera failing,
// answered as temporary.
func Reply(err error) (*gosmtp.SMTPError, bool) {
	for _, r := range replies {
		if errors.Is(err, r.match) {
			return &gosmtp.SMTPError{Code: r.code, EnhancedCode: r.enhanced, Message: r.text}, true
		}
	}

	return &gosmtp.SMTPError{Code: 451, EnhancedCode: gosmtp.EnhancedCode{4, 3, 0}, Message: "Temporary failure, try again later"}, false
}
