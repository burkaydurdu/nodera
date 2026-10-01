// Package events is the notification context's own view of the customer
// context's payloads: the same wire shape, declared here so the two contexts
// never import each other.
package events

import "github.com/google/uuid"

type Recipient struct {
	UserID   uuid.UUID `json:"user_id"`
	Email    string    `json:"email"`
	FullName string    `json:"full_name"`
	Locale   string    `json:"locale"`
}

type EmailVerificationCodeEvent struct {
	Payload struct {
		Recipient
		Code string `json:"code"`
	} `json:"payload"`
}

type EmailAlreadyRegisteredEvent struct {
	Payload struct {
		Recipient
	} `json:"payload"`
}

type MFAEmailCodeEvent struct {
	Payload struct {
		Recipient
		Code string `json:"code"`
	} `json:"payload"`
}

type PasswordResetEvent struct {
	Payload struct {
		Recipient
		ResetURL string `json:"reset_url"`
	} `json:"payload"`
}
