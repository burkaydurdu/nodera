package dto

import (
	"time"

	"github.com/piggystats/nodera/contexts/customer/aggregates/session"
)

type Session struct {
	AccessToken      string    `json:"access_token"`
	RefreshToken     string    `json:"refresh_token"`
	AccessExpiresAt  time.Time `json:"access_expires_at"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
	TokenType        string    `json:"token_type"`
}

func ToSession(pair *session.TokenPair) *Session {
	return &Session{
		AccessToken:      pair.AccessToken,
		RefreshToken:     pair.RefreshToken,
		AccessExpiresAt:  pair.AccessExpiresAt,
		RefreshExpiresAt: pair.RefreshExpiresAt,
		TokenType:        "Bearer",
	}
}

// LoginResponse is either a Login challenge (mfa_required) or a Session.
type LoginResponse struct {
	MFARequired        bool     `json:"mfa_required"`
	ChallengeToken     string   `json:"challenge_token,omitempty"`
	Method             string   `json:"method,omitempty"`
	SupportsResend     bool     `json:"supports_resend,omitempty"`
	ResendAfterSeconds int      `json:"resend_after_seconds,omitempty"`
	Session            *Session `json:"session,omitempty"`
}
