package consumers

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/piggystats/rabbitmq"

	"github.com/piggystats/nodera/contexts/notification/aggregates/notification"
	"github.com/piggystats/nodera/contexts/notification/application/handlers"
	"github.com/piggystats/nodera/contexts/notification/async/constants"
	"github.com/piggystats/nodera/contexts/notification/events"
	"github.com/piggystats/nodera/pkg/authz"
	"github.com/piggystats/nodera/utils/str"
)

//go:generate mockgen -destination=../../mock/consumers/notifier.go -package=mockconsumers github.com/piggystats/nodera/contexts/notification/async/consumers INotifier
type INotifier interface {
	NotifyTeam(ctx context.Context, event handlers.TeamEvent) error
	NotifyPerson(ctx context.Context, event handlers.PersonEvent) error
}

// CustomerEmails makes a Notification of every email the customer context
// asks for. The code or link an email carries rides in its email params only,
// never recorded (docs/adr/0008).
type CustomerEmails struct {
	notifier INotifier
}

func NewCustomerEmails(notifier INotifier) *CustomerEmails {
	return &CustomerEmails{notifier: notifier}
}

func (c *CustomerEmails) RoutingKeys() []string {
	return []string{constants.EmailVerificationCode, constants.EmailAlreadyRegistered, constants.MFAEmailCode, constants.PasswordReset, constants.TeamInvitation}
}

func (c *CustomerEmails) Consume(message *rabbitmq.Message) error {
	event, err := customerEmail(message)
	if err != nil || event == nil {
		return err
	}
	if str.IsBlank(event.Recipient.Email) {
		return notification.ErrorMalformedEvent.Copy().SetMessage("event payload is missing the email address")
	}

	return c.notifier.NotifyPerson(context.Background(), *event)
}

// customerEmail answers nil for a routing key this consumer does not handle.
func customerEmail(message *rabbitmq.Message) (*handlers.PersonEvent, error) {
	switch message.RoutingKey {
	case constants.EmailVerificationCode:
		var e events.EmailVerificationCodeEvent
		if err := decodeEvent(message, &e); err != nil {
			return nil, err
		}

		return signInEmail(message, notification.TypeEmailVerificationCode, e.Payload.Recipient, map[string]any{"Code": e.Payload.Code})
	case constants.EmailAlreadyRegistered:
		var e events.EmailAlreadyRegisteredEvent
		if err := decodeEvent(message, &e); err != nil {
			return nil, err
		}

		return signInEmail(message, notification.TypeEmailAlreadyRegistered, e.Payload.Recipient, map[string]any{})
	case constants.MFAEmailCode:
		var e events.MFAEmailCodeEvent
		if err := decodeEvent(message, &e); err != nil {
			return nil, err
		}

		return signInEmail(message, notification.TypeMFAEmailCode, e.Payload.Recipient, map[string]any{"Code": e.Payload.Code})
	case constants.PasswordReset:
		var e events.PasswordResetEvent
		if err := decodeEvent(message, &e); err != nil {
			return nil, err
		}

		return signInEmail(message, notification.TypePasswordReset, e.Payload.Recipient, map[string]any{"ResetURL": e.Payload.ResetURL})
	case constants.TeamInvitation:
		return invitation(message)
	default:
		return nil, nil
	}
}

// signInEmail is named by its message: these events carry no id of their own,
// and a redelivery keeps the message's.
func signInEmail(message *rabbitmq.Message, t notification.Type, r events.Recipient, emailParams map[string]any) (*handlers.PersonEvent, error) {
	occasion, err := messageID(message)
	if err != nil {
		return nil, err
	}

	return &handlers.PersonEvent{
		SourceKey: string(t) + ":" + occasion, Type: t, Recipient: recipientOf(r), Params: map[string]any{}, EmailParams: emailParams,
	}, nil
}

// invitation belongs to no Team in the Inbox: the invitee is not a member yet.
// One sending is named by the Invitation and its time, so a resend is told
// again and a redelivery is not.
func invitation(message *rabbitmq.Message) (*handlers.PersonEvent, error) {
	var e events.TeamInvitationEvent
	if err := decodeEvent(message, &e); err != nil {
		return nil, err
	}
	p := e.Payload
	// The template names the Role in the reader's language; a Role the matrix
	// does not know has no name, and a redelivery would not grow one.
	if !authz.IsRole(p.Role) {
		return nil, notification.ErrorMalformedEvent.Copy().SetMessage("unknown role in team invitation")
	}

	occasion := p.InvitationID.String() + ":" + p.SentAt.UTC().Format(time.RFC3339Nano)
	if p.InvitationID == uuid.Nil {
		id, err := messageID(message)
		if err != nil {
			return nil, err
		}
		occasion = id
	}

	return &handlers.PersonEvent{
		SourceKey: string(notification.TypeTeamInvitation) + ":" + occasion,
		Type:      notification.TypeTeamInvitation,
		Recipient: recipientOf(p.Recipient),
		Params: map[string]any{
			"invitation_id": p.InvitationID.String(), "team_id": p.TeamID.String(), "team_name": p.TeamName,
			"role": p.Role, "inviter_name": p.InviterName, "expires_at": p.ExpiresAt.UTC(),
		},
		EmailParams: map[string]any{
			"TeamName": p.TeamName, "Role": p.Role, "InviterName": p.InviterName, "AcceptURL": p.AcceptURL, "ExpiresAt": p.ExpiresAt,
		},
	}, nil
}

func messageID(message *rabbitmq.Message) (string, error) {
	if str.IsNotEmpty(message.MessageID) {
		return message.MessageID, nil
	}
	if str.IsNotEmpty(message.CorrelationID) {
		return message.CorrelationID, nil
	}

	return "", notification.ErrorMalformedEvent.Copy().SetMessage("message carries no id")
}

// recipientOf is a person the customer context named; the nil uuid is an
// address with no account behind it.
func recipientOf(r events.Recipient) notification.Recipient {
	recipient := notification.Recipient{Email: r.Email, FullName: r.FullName, Locale: r.Locale}
	if r.UserID != uuid.Nil {
		userID := r.UserID
		recipient.UserID = &userID
	}

	return recipient
}
