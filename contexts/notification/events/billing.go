package events

import (
	"encoding/json"
	"time"
)

// Amount is a money value as billing formatted it: shopspring/decimal goes on
// the wire as a quoted decimal string, which json.Number accepts (as it does a
// bare number), and the email shows the digits exactly as billing rounded them
// rather than re-deriving a float.
type Amount = json.Number

// Billing is the part every billing email shares. Recipients are the invoice
// email and every owner of the Team, each addressed in their own Locale; the
// invoice email has no User behind it, so its UserID is the nil uuid.
type Billing struct {
	Recipients []Recipient `json:"recipients"`
	TeamName   string      `json:"team_name"`
}

type InvoiceIssuedEvent struct {
	Payload struct {
		Billing
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
		InvoiceNumber string `json:"invoice_number"`
		Amount        Amount `json:"amount"`
		Currency      string `json:"currency"`
		ConfirmURL    string `json:"confirm_url"`
	} `json:"payload"`
}

type TeamSuspendedEvent struct {
	Payload struct {
		Billing
		InvoiceNumber string `json:"invoice_number"`
		PayURL        string `json:"pay_url"`
	} `json:"payload"`
}

type DropletsDeletionWarningEvent struct {
	Payload struct {
		Billing
		InvoiceNumber string    `json:"invoice_number"`
		DeletionAt    time.Time `json:"deletion_at"`
		PayURL        string    `json:"pay_url"`
	} `json:"payload"`
}

type CardExpiringEvent struct {
	Payload struct {
		Billing
		Brand    string `json:"brand"`
		Last4    string `json:"last4"`
		ExpMonth int    `json:"exp_month"`
		ExpYear  int    `json:"exp_year"`
	} `json:"payload"`
}
