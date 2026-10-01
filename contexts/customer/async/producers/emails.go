package producers

import (
	"context"

	"github.com/piggystats/nodera/contexts/customer/aggregates/user/mfa"
	"github.com/piggystats/nodera/contexts/customer/async/constants"
	"github.com/piggystats/nodera/contexts/customer/async/events"
)

type EmailVerificationCode struct{ client OutboxClient }

func NewEmailVerificationCode(client OutboxClient) *EmailVerificationCode {
	return &EmailVerificationCode{client: client}
}

func (p *EmailVerificationCode) Publish(ctx context.Context, recipient events.Recipient, code string) error {
	return publish(ctx, p.client, constants.EmailVerificationCode, &events.EmailVerificationCodeEvent{
		Payload: events.EmailVerificationCodeBody{Recipient: recipient, Code: code},
	})
}

type EmailAlreadyRegistered struct{ client OutboxClient }

func NewEmailAlreadyRegistered(client OutboxClient) *EmailAlreadyRegistered {
	return &EmailAlreadyRegistered{client: client}
}

func (p *EmailAlreadyRegistered) Publish(ctx context.Context, recipient events.Recipient) error {
	return publish(ctx, p.client, constants.EmailAlreadyRegistered, &events.EmailAlreadyRegisteredEvent{
		Payload: events.EmailAlreadyRegisteredBody{Recipient: recipient},
	})
}

// MFAEmailCode adapts the outbox to mfa.EmailMailerPublisher so the provider
// stays free of transport details.
type MFAEmailCode struct{ client OutboxClient }

func NewMFAEmailCode(client OutboxClient) *MFAEmailCode {
	return &MFAEmailCode{client: client}
}

func (p *MFAEmailCode) Publish(ctx context.Context, e mfa.EmailMailerEvent) error {
	return publish(ctx, p.client, constants.MFAEmailCode, &events.MFAEmailCodeEvent{
		Payload: events.MFAEmailCodeBody{
			Recipient: events.Recipient{UserID: e.UserID, Email: e.Email, FullName: e.FullName, Locale: e.Locale},
			Code:      e.Code,
		},
	})
}

type PasswordReset struct{ client OutboxClient }

func NewPasswordReset(client OutboxClient) *PasswordReset {
	return &PasswordReset{client: client}
}

func (p *PasswordReset) Publish(ctx context.Context, recipient events.Recipient, resetURL string) error {
	return publish(ctx, p.client, constants.PasswordReset, &events.PasswordResetEvent{
		Payload: events.PasswordResetBody{Recipient: recipient, ResetURL: resetURL},
	})
}

type TeamInvitation struct{ client OutboxClient }

func NewTeamInvitation(client OutboxClient) *TeamInvitation {
	return &TeamInvitation{client: client}
}

func (p *TeamInvitation) Publish(ctx context.Context, body events.TeamInvitationBody) error {
	body.ExpiresAt = body.ExpiresAt.UTC()

	return publish(ctx, p.client, constants.TeamInvitation, &events.TeamInvitationEvent{Payload: body})
}
