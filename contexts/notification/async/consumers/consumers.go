package consumers

import (
	"context"
	"encoding/json"

	"github.com/piggystats/rabbitmq"

	"github.com/piggystats/nodera/contexts/notification/email"
	"github.com/piggystats/nodera/contexts/notification/events"
	"github.com/piggystats/nodera/pkg/authz"
)

// ISender is the slice of the email sender the consumers use.
//
//go:generate mockgen -destination=../../mock/sender.go -package=mocknotification github.com/piggystats/nodera/contexts/notification/async/consumers ISender
type ISender interface {
	Send(ctx context.Context, kind email.Kind, to, recipientLocale string, params map[string]any) error
}

// A malformed payload is terminal: the plain unmarshal error would be retried
// forever, so it is wrapped into a domain error instead.
func decode(message *rabbitmq.Message, into any) error {
	if err := json.Unmarshal(message.Payload, into); err != nil {
		return email.ErrorInvalidRecipient.Copy().SetMessage("malformed event payload").SetError(err)
	}

	return nil
}

func recipientParams(r events.Recipient) map[string]any {
	return map[string]any{"FullName": r.FullName, "Email": r.Email}
}

type EmailVerificationCode struct{ sender ISender }

func NewEmailVerificationCode(sender ISender) *EmailVerificationCode {
	return &EmailVerificationCode{sender: sender}
}

func (c *EmailVerificationCode) Consume(message *rabbitmq.Message) error {
	var event events.EmailVerificationCodeEvent
	if err := decode(message, &event); err != nil {
		return err
	}

	params := recipientParams(event.Payload.Recipient)
	params["Code"] = event.Payload.Code

	return c.sender.Send(context.Background(), email.KindEmailVerificationCode, event.Payload.Email, event.Payload.Locale, params)
}

type EmailAlreadyRegistered struct{ sender ISender }

func NewEmailAlreadyRegistered(sender ISender) *EmailAlreadyRegistered {
	return &EmailAlreadyRegistered{sender: sender}
}

func (c *EmailAlreadyRegistered) Consume(message *rabbitmq.Message) error {
	var event events.EmailAlreadyRegisteredEvent
	if err := decode(message, &event); err != nil {
		return err
	}

	return c.sender.Send(context.Background(), email.KindEmailAlreadyRegistered, event.Payload.Email, event.Payload.Locale, recipientParams(event.Payload.Recipient))
}

type MFAEmailCode struct{ sender ISender }

func NewMFAEmailCode(sender ISender) *MFAEmailCode {
	return &MFAEmailCode{sender: sender}
}

func (c *MFAEmailCode) Consume(message *rabbitmq.Message) error {
	var event events.MFAEmailCodeEvent
	if err := decode(message, &event); err != nil {
		return err
	}

	params := recipientParams(event.Payload.Recipient)
	params["Code"] = event.Payload.Code

	return c.sender.Send(context.Background(), email.KindMFAEmailCode, event.Payload.Email, event.Payload.Locale, params)
}

type PasswordReset struct{ sender ISender }

func NewPasswordReset(sender ISender) *PasswordReset {
	return &PasswordReset{sender: sender}
}

func (c *PasswordReset) Consume(message *rabbitmq.Message) error {
	var event events.PasswordResetEvent
	if err := decode(message, &event); err != nil {
		return err
	}

	params := recipientParams(event.Payload.Recipient)
	params["ResetURL"] = event.Payload.ResetURL

	return c.sender.Send(context.Background(), email.KindPasswordReset, event.Payload.Email, event.Payload.Locale, params)
}

type TeamInvitation struct{ sender ISender }

func NewTeamInvitation(sender ISender) *TeamInvitation {
	return &TeamInvitation{sender: sender}
}

func (c *TeamInvitation) Consume(message *rabbitmq.Message) error {
	var event events.TeamInvitationEvent
	if err := decode(message, &event); err != nil {
		return err
	}

	// The template names the Role in the reader's language; a Role the matrix
	// does not know has no name, and a redelivery would not grow one.
	if !authz.IsRole(event.Payload.Role) {
		return email.ErrorInvalidRecipient.Copy().SetMessage("unknown role in team invitation")
	}

	params := recipientParams(event.Payload.Recipient)
	params["TeamName"] = event.Payload.TeamName
	params["Role"] = event.Payload.Role
	params["InviterName"] = event.Payload.InviterName
	params["AcceptURL"] = event.Payload.AcceptURL
	params["ExpiresAt"] = event.Payload.ExpiresAt

	return c.sender.Send(context.Background(), email.KindTeamInvitation, event.Payload.Recipient.Email, event.Payload.Recipient.Locale, params)
}
