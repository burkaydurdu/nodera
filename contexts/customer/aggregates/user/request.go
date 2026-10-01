package user

import (
	"strings"

	"github.com/piggystats/nodera/contexts/customer/aggregates/user/mfa"
	"github.com/piggystats/nodera/pkg/country"
)

type RegisterRequest struct {
	Email    string `json:"email" validate:"required,email,max=255"`
	Password string `json:"password" validate:"required,password"`
	FullName string `json:"full_name" validate:"required,min=1,max=255"`
	Country  string `json:"country" validate:"required,country"`
	Locale   string `json:"locale" validate:"required,locale"`
	// InvitationToken is the secret from an Invitation email, when the
	// sign-up started from one.
	InvitationToken string `json:"invitation_token" validate:"omitempty,max=128"`
}

// Normalize folds the email and the country code so the same address never
// registers twice under a different spelling.
func (r *RegisterRequest) Normalize() {
	r.Email = NormalizeEmail(r.Email)
	r.Country = country.Normalize(r.Country)
	r.FullName = strings.TrimSpace(r.FullName)
}

type VerifyEmailRequest struct {
	Email string `json:"email" validate:"required,email"`
	Code  string `json:"code" validate:"required,len=6,numeric"`
}

func (r *VerifyEmailRequest) Normalize() { r.Email = NormalizeEmail(r.Email) }

type ResendVerificationRequest struct {
	Email string `json:"email" validate:"required,email"`
}

func (r *ResendVerificationRequest) Normalize() { r.Email = NormalizeEmail(r.Email) }

type LoginRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

func (r *LoginRequest) Normalize() { r.Email = NormalizeEmail(r.Email) }

type MFAVerifyRequest struct {
	ChallengeToken string `json:"challenge_token" validate:"required"`
	Code           string `json:"code" validate:"required,len=6,numeric"`
}

type MFAResendRequest struct {
	ChallengeToken string `json:"challenge_token" validate:"required"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token" validate:"required"`
}

type ForgotPasswordRequest struct {
	Email string `json:"email" validate:"required,email"`
}

func (r *ForgotPasswordRequest) Normalize() { r.Email = NormalizeEmail(r.Email) }

type ResetPasswordRequest struct {
	Token           string `json:"token" validate:"required"`
	Password        string `json:"password" validate:"required,password"`
	PasswordConfirm string `json:"password_confirm" validate:"required,eqfield=Password"`
}

type UpdateMeRequest struct {
	FullName string `json:"full_name" validate:"required,min=1,max=255"`
	Locale   string `json:"locale" validate:"required,locale"`
	Country  string `json:"country" validate:"required,country"`
}

func (r *UpdateMeRequest) Normalize() {
	r.Country = country.Normalize(r.Country)
	r.FullName = strings.TrimSpace(r.FullName)
}

type ChangePasswordRequest struct {
	CurrentPassword    string `json:"current_password" validate:"required"`
	NewPassword        string `json:"new_password" validate:"required,password"`
	NewPasswordConfirm string `json:"new_password_confirm" validate:"required,eqfield=NewPassword"`
}

type UpdateMFARequest struct {
	Method   mfa.Method `json:"method" validate:"required,oneof=none email"`
	Password string     `json:"password" validate:"required"`
}

type DeleteSessionRequest struct {
	SessionID string `param:"sessionID" json:"-" validate:"required,uuid"`
}

func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
