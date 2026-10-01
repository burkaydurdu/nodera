package mfa

import (
	"context"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/customer/aggregates/user/otp"
)

// EmailMailerEvent is what the provider asks the publisher to send.
type EmailMailerEvent struct {
	UserID   uuid.UUID
	Email    string
	FullName string
	Locale   string
	Code     string
}

// EmailMailerPublisher abstracts the outbox producer so this package stays
// free of transport concerns.
//
//go:generate mockgen -destination=../../../mock/producers/mfa_email_publisher.go -package=mockproducers github.com/piggystats/nodera/contexts/customer/aggregates/user/mfa EmailMailerPublisher
type EmailMailerPublisher interface {
	Publish(ctx context.Context, event EmailMailerEvent) error
}

// EmailProvider implements Provider with a 6-digit code delivered by email.
type EmailProvider struct {
	codes     *otp.Store
	publisher EmailMailerPublisher
}

func NewEmailProvider(codes *otp.Store, publisher EmailMailerPublisher) *EmailProvider {
	return &EmailProvider{codes: codes, publisher: publisher}
}

func (p *EmailProvider) Method() Method          { return MethodEmail }
func (p *EmailProvider) SupportsResend() bool    { return true }
func (p *EmailProvider) ResendAfterSeconds() int { return p.codes.CooldownSeconds() }

func (p *EmailProvider) IssueChallenge(ctx context.Context, c ChallengeContext) error {
	code, err := p.codes.Renew(ctx, c.UserID.String())
	if err != nil {
		return err
	}

	return p.send(ctx, c, code)
}

func (p *EmailProvider) ResendChallenge(ctx context.Context, c ChallengeContext) error {
	code, err := p.codes.Issue(ctx, c.UserID.String())
	if err != nil {
		return err
	}

	return p.send(ctx, c, code)
}

func (p *EmailProvider) send(ctx context.Context, c ChallengeContext, code string) error {
	return p.publisher.Publish(ctx, EmailMailerEvent{
		UserID:   c.UserID,
		Email:    c.Email,
		FullName: c.FullName,
		Locale:   c.Locale,
		Code:     code,
	})
}

func (p *EmailProvider) VerifyCode(ctx context.Context, c ChallengeContext, code string) error {
	return p.codes.Verify(ctx, c.UserID.String(), code)
}
