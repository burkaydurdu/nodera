package payment

import "net/http"

// WebhookRequest is a provider notification as it arrived. It is read, not
// bound: the signature covers the raw body, so the body is never re-encoded.
type WebhookRequest struct {
	Provider string
	ClientIP string
	Headers  http.Header
	Body     []byte
}

// ConfirmRequest is the public link the confirmation email carries; the
// token authorizes this one Payment, so no session is needed.
type ConfirmRequest struct {
	PaymentID string `param:"paymentID" json:"-" validate:"required,uuid"`
	Token     string `query:"token" json:"-" validate:"required,hexadecimal,max=128"`
}

// StartConfirmationRequest opens the 3-D Secure step from the confirmation
// page; CallbackRequest is the provider's return to the service. Both are
// authorized by the link's token, never a session.
type StartConfirmationRequest struct {
	PaymentID string `param:"paymentID" json:"-" validate:"required,uuid"`
	Token     string `query:"token" json:"-" validate:"required,hexadecimal,max=128"`
}

type CallbackRequest struct {
	PaymentID string `param:"paymentID" json:"-" validate:"required,uuid"`
	Token     string `query:"token" json:"-" validate:"required,hexadecimal,max=128"`
	// Callback is the provider's posted form, read rather than bound: its
	// hash covers the raw fields.
	Callback map[string]string `json:"-"`
}
