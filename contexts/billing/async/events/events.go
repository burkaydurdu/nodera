// Package events is the billing context's own view of the payloads it
// consumes and the ones it publishes: the same wire shape as the producing
// context declares, so the contexts never import each other (docs/adr/0004).
package events

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Consumed: customer context
// ---------------------------------------------------------------------------

// TeamCreatedEvent carries the owner's contact details since phase 4; an
// older producer leaves them empty and the read model fills them from the
// owner's TeamMemberAdded instead.
type TeamCreatedEvent struct {
	Payload struct {
		TeamID        uuid.UUID `json:"team_id"`
		OwnerUserID   uuid.UUID `json:"owner_user_id"`
		Name          string    `json:"name"`
		OwnerEmail    string    `json:"owner_email"`
		OwnerFullName string    `json:"owner_full_name"`
		OwnerLocale   string    `json:"owner_locale"`
		CreatedAt     time.Time `json:"created_at"`
	} `json:"payload"`
}

type TeamMemberAddedEvent struct {
	Payload struct {
		TeamID   uuid.UUID `json:"team_id"`
		UserID   uuid.UUID `json:"user_id"`
		Role     string    `json:"role"`
		Email    string    `json:"email"`
		FullName string    `json:"full_name"`
		Locale   string    `json:"locale"`
		AddedAt  time.Time `json:"added_at"`
	} `json:"payload"`
}

type TeamMemberRoleChangedEvent struct {
	Payload struct {
		TeamID    uuid.UUID `json:"team_id"`
		UserID    uuid.UUID `json:"user_id"`
		Role      string    `json:"role"`
		ChangedAt time.Time `json:"changed_at"`
	} `json:"payload"`
}

type TeamMemberRemovedEvent struct {
	Payload struct {
		TeamID    uuid.UUID `json:"team_id"`
		UserID    uuid.UUID `json:"user_id"`
		RemovedAt time.Time `json:"removed_at"`
	} `json:"payload"`
}

// ---------------------------------------------------------------------------
// Consumed: droplet context (usage)
// ---------------------------------------------------------------------------

type DropletBillingStartedEvent struct {
	Payload struct {
		DropletID   uuid.UUID `json:"droplet_id"`
		TeamID      uuid.UUID `json:"team_id"`
		ProjectID   uuid.UUID `json:"project_id"`
		Hostname    string    `json:"hostname"`
		PlanCode    string    `json:"plan_code"`
		HourlyPrice string    `json:"hourly_price"`
		StartedAt   time.Time `json:"started_at"`
	} `json:"payload"`
}

type DropletBillingStoppedEvent struct {
	Payload struct {
		DropletID uuid.UUID `json:"droplet_id"`
		TeamID    uuid.UUID `json:"team_id"`
		StoppedAt time.Time `json:"stopped_at"`
	} `json:"payload"`
}

// ---------------------------------------------------------------------------
// Consumed: mail context (Packages)
// ---------------------------------------------------------------------------

// MailPackageStartedEvent opens the interval (project_id, started_at); the
// monthly price is USD with two decimals, copied when the Package was chosen.
type MailPackageStartedEvent struct {
	Payload struct {
		ProjectID     uuid.UUID `json:"project_id"`
		TeamID        uuid.UUID `json:"team_id"`
		ProjectName   string    `json:"project_name"`
		PackageCode   string    `json:"package_code"`
		MonthlyEmails int       `json:"monthly_emails"`
		MonthlyPrice  string    `json:"monthly_price"`
		StartedAt     time.Time `json:"started_at"`
	} `json:"payload"`
}

// MailPackageStoppedEvent closes the interval its started_at names.
type MailPackageStoppedEvent struct {
	Payload struct {
		ProjectID uuid.UUID `json:"project_id"`
		TeamID    uuid.UUID `json:"team_id"`
		StartedAt time.Time `json:"started_at"`
		StoppedAt time.Time `json:"stopped_at"`
	} `json:"payload"`
}

// ---------------------------------------------------------------------------
// Consumed: storage context (Packages)
// ---------------------------------------------------------------------------

// StoragePackageStartedEvent opens the interval (storage_id, started_at); the
// monthly price is USD with two decimals, copied when the Package was chosen.
type StoragePackageStartedEvent struct {
	Payload struct {
		StorageID    uuid.UUID `json:"storage_id"`
		TeamID       uuid.UUID `json:"team_id"`
		ProjectID    uuid.UUID `json:"project_id"`
		ProjectName  string    `json:"project_name"`
		BucketName   string    `json:"bucket_name"`
		Region       string    `json:"region"`
		PackageCode  string    `json:"package_code"`
		SizeBytes    int64     `json:"size_bytes"`
		MonthlyPrice string    `json:"monthly_price"`
		StartedAt    time.Time `json:"started_at"`
	} `json:"payload"`
}

// StoragePackageStoppedEvent closes the interval its started_at names.
type StoragePackageStoppedEvent struct {
	Payload struct {
		StorageID uuid.UUID `json:"storage_id"`
		TeamID    uuid.UUID `json:"team_id"`
		StartedAt time.Time `json:"started_at"`
		StoppedAt time.Time `json:"stopped_at"`
	} `json:"payload"`
}

// ---------------------------------------------------------------------------
// Published
// ---------------------------------------------------------------------------

// Address is the billing profile's invoice address: told about the Team's
// money matters although no member holds it, in the Locale the profile's
// country writes Invoices in.
type Address struct {
	Email    string `json:"email"`
	FullName string `json:"full_name"`
	Locale   string `json:"locale"`
}

type TeamBillingStatusChangedBody struct {
	TeamID    uuid.UUID `json:"team_id"`
	Status    string    `json:"status"`
	ChangedAt time.Time `json:"changed_at"`
}

type TeamBillingStatusChangedEvent struct {
	Payload TeamBillingStatusChangedBody `json:"payload"`
}

type TeamDropletsDeletionRequestedBody struct {
	TeamID      uuid.UUID `json:"team_id"`
	RequestedAt time.Time `json:"requested_at"`
}

type TeamDropletsDeletionRequestedEvent struct {
	Payload TeamDropletsDeletionRequestedBody `json:"payload"`
}

// The events below are published by collection (invoices, payments, dunning)
// for the notification context. They say what happened to which Team and name
// the thing it happened to; who among the members is told is the notification
// context's decision (docs/adr/0008). The links are signed or personal, so
// they ride only here, for the email, and are never recorded.

type InvoiceIssuedBody struct {
	TeamID           uuid.UUID `json:"team_id"`
	InvoiceID        uuid.UUID `json:"invoice_id"`
	InvoiceAddress   *Address  `json:"invoice_address,omitempty"`
	InvoiceNumber    string    `json:"invoice_number"`
	PeriodStart      time.Time `json:"period_start"`
	PeriodEnd        time.Time `json:"period_end"`
	TotalDue         string    `json:"total_due"`
	Currency         string    `json:"currency"`
	SettlementAmount *string   `json:"settlement_amount,omitempty"`
	SettlementCurr   *string   `json:"settlement_currency,omitempty"`
	PDFURL           string    `json:"pdf_url"`
}

type InvoiceIssuedEvent struct {
	Payload InvoiceIssuedBody `json:"payload"`
}

type PaymentSucceededBody struct {
	TeamID         uuid.UUID `json:"team_id"`
	InvoiceID      uuid.UUID `json:"invoice_id"`
	PaymentID      uuid.UUID `json:"payment_id"`
	InvoiceAddress *Address  `json:"invoice_address,omitempty"`
	InvoiceNumber  string    `json:"invoice_number"`
	Amount         string    `json:"amount"`
	Currency       string    `json:"currency"`
	PaidAt         time.Time `json:"paid_at"`
}

type PaymentSucceededEvent struct {
	Payload PaymentSucceededBody `json:"payload"`
}

type PaymentFailedBody struct {
	TeamID         uuid.UUID  `json:"team_id"`
	InvoiceID      uuid.UUID  `json:"invoice_id"`
	PaymentID      uuid.UUID  `json:"payment_id"`
	InvoiceAddress *Address   `json:"invoice_address,omitempty"`
	InvoiceNumber  string     `json:"invoice_number"`
	Amount         string     `json:"amount"`
	Currency       string     `json:"currency"`
	FailureReason  string     `json:"failure_reason"`
	Attempt        int        `json:"attempt"`
	NextRetryAt    *time.Time `json:"next_retry_at,omitempty"`
}

type PaymentFailedEvent struct {
	Payload PaymentFailedBody `json:"payload"`
}

type PaymentConfirmationRequiredBody struct {
	TeamID         uuid.UUID `json:"team_id"`
	InvoiceID      uuid.UUID `json:"invoice_id"`
	PaymentID      uuid.UUID `json:"payment_id"`
	InvoiceAddress *Address  `json:"invoice_address,omitempty"`
	InvoiceNumber  string    `json:"invoice_number"`
	Amount         string    `json:"amount"`
	Currency       string    `json:"currency"`
	ConfirmURL     string    `json:"confirm_url"`
}

type PaymentConfirmationRequiredEvent struct {
	Payload PaymentConfirmationRequiredBody `json:"payload"`
}

type TeamSuspendedBody struct {
	TeamID         uuid.UUID `json:"team_id"`
	InvoiceID      uuid.UUID `json:"invoice_id"`
	InvoiceAddress *Address  `json:"invoice_address,omitempty"`
	InvoiceNumber  string    `json:"invoice_number"`
	PayURL         string    `json:"pay_url"`
}

type TeamSuspendedEvent struct {
	Payload TeamSuspendedBody `json:"payload"`
}

type DropletsDeletionWarningBody struct {
	TeamID         uuid.UUID `json:"team_id"`
	InvoiceID      uuid.UUID `json:"invoice_id"`
	InvoiceAddress *Address  `json:"invoice_address,omitempty"`
	// DunningDay tells the two warnings of one Invoice apart.
	DunningDay    int       `json:"dunning_day"`
	InvoiceNumber string    `json:"invoice_number"`
	DeletionAt    time.Time `json:"deletion_at"`
	PayURL        string    `json:"pay_url"`
}

type DropletsDeletionWarningEvent struct {
	Payload DropletsDeletionWarningBody `json:"payload"`
}

type CardExpiringBody struct {
	TeamID          uuid.UUID `json:"team_id"`
	PaymentMethodID uuid.UUID `json:"payment_method_id"`
	InvoiceAddress  *Address  `json:"invoice_address,omitempty"`
	Brand           string    `json:"brand"`
	Last4           string    `json:"last4"`
	ExpMonth        int       `json:"exp_month"`
	ExpYear         int       `json:"exp_year"`
}

type CardExpiringEvent struct {
	Payload CardExpiringBody `json:"payload"`
}

func ToJSONString(event any) (string, error) {
	data, err := json.Marshal(event)
	if err != nil {
		return "", err
	}

	return string(data), nil
}
