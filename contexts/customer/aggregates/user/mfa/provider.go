package mfa

import (
	"context"

	"github.com/google/uuid"
)

// ChallengeContext is everything a provider needs to issue or verify a
// challenge for one User.
type ChallengeContext struct {
	UserID   uuid.UUID
	Email    string
	FullName string
	Locale   string
}

// Provider is the contract any MFA mechanism implements; the Registry maps a
// Method to it so the sign-in flow never switches on the method.
type Provider interface {
	Method() Method
	SupportsResend() bool
	ResendAfterSeconds() int
	// IssueChallenge starts a fresh challenge after a correct password.
	IssueChallenge(ctx context.Context, c ChallengeContext) error
	// ResendChallenge re-sends for an open challenge, within the cooldown.
	ResendChallenge(ctx context.Context, c ChallengeContext) error
	VerifyCode(ctx context.Context, c ChallengeContext, code string) error
}
