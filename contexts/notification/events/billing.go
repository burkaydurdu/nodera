package events

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Amount is a money value as billing formatted it: shopspring/decimal goes on
// the wire as a quoted decimal string, which json.Number accepts (as it does a
// bare number), and the email shows the digits exactly as billing rounded them
// rather than re-deriving a float.
type Amount = json.Number

// Billing is the part every billing event shares: the Team it happened to and
// the billing profile's invoice address, told as well although no member
// holds it. Who among the members is told is this context's decision
// (docs/adr/0008).
type Billing struct {
	TeamID         uuid.UUID `json:"team_id"`
	InvoiceAddress *Address  `json:"invoice_address,omitempty"`
}

// Address is an email address with the name and Locale to write to it in.
type Address struct {
	Email    string `json:"email"`
	FullName string `json:"full_name"`
	Locale   string `json:"locale"`
}

type InvoiceIssuedEvent struct {
	Payload struct {
		Billing
		InvoiceID          uuid.UUID `json:"invoice_id"`
		InvoiceNumber      string    `json:"invoice_number"`
		PeriodStart        time.Time `json:"period_start"`
		PeriodEnd          time.Time `json:"period_end"`
		TotalDue           Amount    `json:"total_due"`
		Currency           string    `json:"currency"`
		SettlementAmount   Amount    `json:"settlement_amount,omitempty"`
		SettlementCurrency string    `json:"settlement_currency,omitempty"`
		PdfURL             string    `json:"pdf_url"`
	} `json:"payload"`
}

type PaymentSucceededEvent struct {
	Payload struct {
		Billing
		InvoiceID     uuid.UUID `json:"invoice_id"`
		PaymentID     uuid.UUID `json:"payment_id"`
		InvoiceNumber string    `json:"invoice_number"`
		Amount        Amount    `json:"amount"`
		Currency      string    `json:"currency"`
		PaidAt        time.Time `json:"paid_at"`
	} `json:"payload"`
}

// PaymentFailedEvent carries no NextRetryAt once the Dunning schedule is
// exhausted; the email then stops promising another attempt.
type PaymentFailedEvent struct {
	Payload struct {
		Billing
		InvoiceID     uuid.UUID  `json:"invoice_id"`
		PaymentID     uuid.UUID  `json:"payment_id"`
		InvoiceNumber string     `json:"invoice_number"`
		Amount        Amount     `json:"amount"`
		Currency      string     `json:"currency"`
		FailureReason string     `json:"failure_reason"`
		Attempt       int        `json:"attempt"`
		NextRetryAt   *time.Time `json:"next_retry_at,omitempty"`
	} `json:"payload"`
}

type PaymentConfirmationRequiredEvent struct {
	Payload struct {
		Billing
		InvoiceID     uuid.UUID `json:"invoice_id"`
		PaymentID     uuid.UUID `json:"payment_id"`
		InvoiceNumber string    `json:"invoice_number"`
		Amount        Amount    `json:"amount"`
		Currency      string    `json:"currency"`
		ConfirmURL    string    `json:"confirm_url"`
	} `json:"payload"`
}

type TeamSuspendedEvent struct {
	Payload struct {
		Billing
		InvoiceID     uuid.UUID `json:"invoice_id"`
		InvoiceNumber string    `json:"invoice_number"`
		PayURL        string    `json:"pay_url"`
	} `json:"payload"`
}

type DropletsDeletionWarningEvent struct {
	Payload struct {
		Billing
		InvoiceID     uuid.UUID `json:"invoice_id"`
		DunningDay    int       `json:"dunning_day"`
		InvoiceNumber string    `json:"invoice_number"`
		DeletionAt    time.Time `json:"deletion_at"`
		PayURL        string    `json:"pay_url"`
	} `json:"payload"`
}

type CardExpiringEvent struct {
	Payload struct {
		Billing
		PaymentMethodID uuid.UUID `json:"payment_method_id"`
		Brand           string    `json:"brand"`
		Last4           string    `json:"last4"`
		ExpMonth        int       `json:"exp_month"`
		ExpYear         int       `json:"exp_year"`
	} `json:"payload"`
}
