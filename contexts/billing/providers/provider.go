// Package providers is the seam between the billing context and the Payment
// providers it charges cards through (ADR-0006). Each adapter keeps its
// provider's vocabulary, references and errors inside and answers in
// Nodera's words; a provider name never leaves the adapter's package except
// as the routing code stored on a Payment method.
package providers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// CallTimeout bounds one call to a provider. Reconciliation waits twice as
// long before it treats an unanswered attempt as lost, so it never races a
// call still in flight.
const CallTimeout = 30 * time.Second

// Payer is what a provider may need to know about the Team a card is saved
// for: the Billing profile's identity and address, never more.
type Payer struct {
	TeamID       uuid.UUID
	TeamName     string
	LegalName    string
	Email        string
	Country      string
	City         string
	AddressLine1 string
	PostalCode   string
	TaxID        string
}

// VerificationRequest starts a hosted card verification. ReferenceID is
// Nodera's own verification id, sent along so the provider's record links
// back; ReturnURL is where the provider sends the browser afterwards.
type VerificationRequest struct {
	Payer       Payer
	Currency    string
	ReturnURL   string
	ReferenceID string
}

// Verification is what the web app needs to open the hosted form.
type Verification struct {
	Ref         string
	RedirectURL string
}

// SavedCard is a verified card as the provider holds it: the references
// Nodera charges it by and what a customer needs to tell cards apart.
type SavedCard struct {
	CustomerRef string
	CardRef     string
	Brand       string
	Last4       string
	ExpMonth    int
	ExpYear     int
}

// CardRef names a saved card at its provider.
type CardRef struct {
	CustomerRef string
	CardRef     string
}

// ChargeRequest charges a saved card. IdempotencyKey is
// `invoice:{id}:attempt:{n}`: providers that do not dedupe by it still record
// it, so a lost answer can be looked up rather than charged again.
// ReturnURL is where a 3-D Secure confirmation calls back; ProviderPaymentRef
// names the refused charge a confirmation continues, for providers that
// continue rather than start over.
type ChargeRequest struct {
	Card               CardRef
	Amount             decimal.Decimal
	Currency           string
	IdempotencyKey     string
	Description        string
	ReturnURL          string
	ProviderPaymentRef string
}

// PaymentState is a charge's state in Nodera's words.
type PaymentState string

const (
	StatePending              PaymentState = "pending"
	StateSucceeded            PaymentState = "succeeded"
	StateFailed               PaymentState = "failed"
	StateConfirmationRequired PaymentState = "confirmation_required"
)

// ChargeResult is the outcome of a charge, a confirmation, a fetch or a
// search. FailureReason is customer-safe (a decline category, never the
// provider's raw text). IdempotencyKey is the attempt the provider's record
// belongs to, when the provider reports it.
type ChargeResult struct {
	State              PaymentState
	ProviderPaymentRef string
	FailureReason      string
	IdempotencyKey     string
}

// Confirmation is the 3-D Secure step opened for the cardholder: a page to
// redirect to, or an HTML document to render, never neither.
type Confirmation struct {
	ProviderPaymentRef string
	RedirectURL        string
	HTML               string
}

// CardStatus is what the provider knows about a saved card's expiry. The
// month and year are 0 when it does not report them; ExpiringSoon is its own
// judgement that the card expires next month.
type CardStatus struct {
	ExpMonth     int
	ExpYear      int
	ExpiringSoon bool
	Expired      bool
}

type RefundRequest struct {
	ProviderPaymentRef string
	Amount             decimal.Decimal
	Currency           string
	IdempotencyKey     string
}

type RefundResult struct {
	ProviderRefundRef string
	State             PaymentState
}

// WebhookKind groups provider notifications by what reconciliation does
// with them.
type WebhookKind string

const (
	WebhookPayment WebhookKind = "payment"
	WebhookRefund  WebhookKind = "refund"
	WebhookOther   WebhookKind = "other"
)

// WebhookEvent is a verified provider notification, provider-neutral. Its
// State is a hint: reconciliation asks FetchPayment before it acts.
type WebhookEvent struct {
	ProviderEventID    string
	Kind               WebhookKind
	ProviderPaymentRef string
	State              PaymentState
}

// Customer-safe decline categories an adapter maps its provider's codes to.
const (
	ReasonDeclined          = "card_declined"
	ReasonInsufficientFunds = "insufficient_funds"
	ReasonExpiredCard       = "expired_card"
	ReasonInvalidCard       = "invalid_card"
	ReasonFraudSuspected    = "fraud_suspected"
	ReasonNotCompleted      = "not_completed"
)

var (
	// ErrUnknownRef: the provider does not know the reference.
	ErrUnknownRef = errors.New("unknown provider reference")
	// ErrBadSignature: a webhook whose signature does not verify.
	ErrBadSignature = errors.New("webhook signature does not verify")
	// ErrNotConfigured: the binary has no adapter for the code.
	ErrNotConfigured = errors.New("payment provider not configured")
)

// DeclinedError is a card the provider refused, with the customer-safe
// category of why. Anything else an adapter returns is the provider being
// unreachable or broken.
type DeclinedError struct {
	Reason string
}

func (e *DeclinedError) Error() string { return "declined: " + e.Reason }

// Declined answers the decline category when err is a DeclinedError.
func Declined(err error) (string, bool) {
	var declined *DeclinedError
	if errors.As(err, &declined) {
		return declined.Reason, true
	}

	return "", false
}

//go:generate mockgen -destination=../mock/providers/provider.go -package=mockproviders github.com/piggystats/nodera/contexts/billing/providers Provider
type Provider interface {
	// Code is the routing code payment_routes and Payment methods store.
	Code() string
	StartCardVerification(ctx context.Context, req VerificationRequest) (*Verification, error)
	// CompleteCardVerification reads the outcome of a hosted verification;
	// callback is whatever the provider appended to the return URL.
	CompleteCardVerification(ctx context.Context, ref string, callback map[string]string) (*SavedCard, error)
	// Charge charges a saved card without the cardholder. A bank refusal that
	// 3-D Secure can turn around answers StateConfirmationRequired without
	// starting anything; only a bank decline is a DeclinedError or
	// StateFailed, any other failure is an error (the provider unavailable).
	Charge(ctx context.Context, req ChargeRequest) (*ChargeResult, error)
	// StartConfirmation opens the 3-D Secure step when the cardholder is
	// there to take it; req.ReturnURL is where the provider calls back.
	StartConfirmation(ctx context.Context, req ChargeRequest) (*Confirmation, error)
	// CompleteConfirmation checks the provider's callback is authentic
	// (ErrBadSignature otherwise) and finishes the charge it reports on.
	CompleteConfirmation(ctx context.Context, callback map[string]string) (*ChargeResult, error)
	// FindCharge looks an attempt up by its idempotency key, so an answer
	// lost in transit is adopted instead of charged again; ErrUnknownRef when
	// the provider holds nothing for it.
	FindCharge(ctx context.Context, idempotencyKey string) (*ChargeResult, error)
	// CardStatus answers what the provider knows about a card's expiry.
	CardStatus(ctx context.Context, card CardRef) (*CardStatus, error)
	// Refund is modelled, not exposed this phase.
	Refund(ctx context.Context, req RefundRequest) (*RefundResult, error)
	// VerifyWebhook checks the signature and decodes the notification;
	// ErrBadSignature when it does not verify.
	VerifyWebhook(ctx context.Context, headers http.Header, body []byte) (*WebhookEvent, error)
	// FetchPayment asks the provider for a charge's current state, the
	// source of truth reconciliation trusts over a webhook body.
	FetchPayment(ctx context.Context, ref string) (*ChargeResult, error)
}

// Registry holds the configured adapters by routing code.
type Registry struct {
	byCode map[string]Provider
}

func NewRegistry(adapters ...Provider) *Registry {
	r := &Registry{byCode: make(map[string]Provider, len(adapters))}
	for _, a := range adapters {
		r.byCode[a.Code()] = a
	}

	return r
}

func (r *Registry) Register(p Provider) {
	r.byCode[p.Code()] = p
}

// Get answers the adapter for a routing code; a code the routing table names
// but the binary has no adapter for is a configuration error.
func (r *Registry) Get(code string) (Provider, error) {
	p, ok := r.byCode[code]
	if !ok {
		return nil, ErrNotConfigured
	}

	return p, nil
}
