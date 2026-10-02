// Package payment is one attempt to collect an Invoice from a Payment method,
// with the provider's result, and the provider webhooks that report on it.
package payment

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type Status string

const (
	StatusPending              Status = "pending"
	StatusSucceeded            Status = "succeeded"
	StatusFailed               Status = "failed"
	StatusConfirmationRequired Status = "confirmation_required"
)

// Trigger is what started the attempt: the issue of the Invoice, a Dunning
// retry, or a person paying now.
type Trigger string

const (
	TriggerIssue   Trigger = "issue"
	TriggerDunning Trigger = "dunning"
	TriggerPayNow  Trigger = "pay_now"
)

// ReasonNoPaymentMethod fails an attempt for a Team without a card to charge.
const ReasonNoPaymentMethod = "no_payment_method"

// ReasonConfirmationExpired fails an attempt whose cardholder never
// confirmed it within the link's lifetime.
const ReasonConfirmationExpired = "confirmation_expired"

// ReasonCurrencyMismatch fails an attempt on a card whose currency the
// Invoice has no amount in.
const ReasonCurrencyMismatch = "currency_mismatch"

type Payment struct {
	ID               uuid.UUID
	InvoiceID        uuid.UUID
	TeamID           uuid.UUID
	PaymentMethodID  *uuid.UUID
	Provider         string
	Attempt          int
	IdempotencyKey   string
	Trigger          Trigger
	Amount           decimal.Decimal
	Currency         string
	Status           Status
	ProviderRef      string
	FailureReason    string
	ConfirmationURL  string
	ConfirmExpiresAt *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
	CompletedAt      *time.Time
}

// IdempotencyKey is what the provider dedupes a charge by, so a retried call
// for the same attempt never charges twice.
func IdempotencyKey(invoiceID uuid.UUID, attempt int) string {
	return fmt.Sprintf("invoice:%s:attempt:%d", invoiceID, attempt)
}

// Settled reports whether the provider's answer is final.
func (p *Payment) Settled() bool {
	return p.Status == StatusSucceeded || p.Status == StatusFailed
}

// Live reports an attempt that may still collect: waiting for the provider,
// or for a cardholder whose confirmation link has not lapsed.
func (p *Payment) Live(at time.Time) bool {
	switch p.Status {
	case StatusPending:
		return true
	case StatusConfirmationRequired:
		return p.ConfirmExpiresAt != nil && at.Before(*p.ConfirmExpiresAt)
	default:
		return false
	}
}

// WebhookEvent is a provider notification that passed its signature check,
// decoded into Nodera's words. It is stored once by the provider's event id;
// reconciliation asks the provider's API about ProviderPaymentRef rather than
// trusting Status.
type WebhookEvent struct {
	ID                 uuid.UUID
	Provider           string
	ProviderEventID    string
	Kind               string
	ProviderPaymentRef string
	Status             string
	Body               []byte
	ReceivedAt         time.Time
	ProcessedAt        *time.Time
}
