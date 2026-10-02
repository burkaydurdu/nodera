// Package dto is what the billing API answers. Money is a decimal string,
// timestamps RFC 3339 UTC, and no field ever names a Payment provider.
package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/billing/aggregates/billingstatus"
	"github.com/piggystats/nodera/contexts/billing/aggregates/paymentmethod"
	"github.com/piggystats/nodera/contexts/billing/aggregates/profile"
)

type Profile struct {
	TeamID       uuid.UUID `json:"team_id"`
	AccountType  string    `json:"account_type" enums:"individual,company"`
	LegalName    string    `json:"legal_name"`
	Country      string    `json:"country" example:"TR"`
	AddressLine1 string    `json:"address_line1"`
	AddressLine2 string    `json:"address_line2"`
	City         string    `json:"city"`
	PostalCode   string    `json:"postal_code"`
	TaxID        string    `json:"tax_id"`
	TaxOffice    string    `json:"tax_office"`
	VATID        string    `json:"vat_id"`
	InvoiceEmail string    `json:"invoice_email"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func ToProfile(p *profile.Profile) *Profile {
	return &Profile{
		TeamID:       p.TeamID,
		AccountType:  string(p.AccountType),
		LegalName:    p.LegalName,
		Country:      p.Country,
		AddressLine1: p.AddressLine1,
		AddressLine2: p.AddressLine2,
		City:         p.City,
		PostalCode:   p.PostalCode,
		TaxID:        p.TaxID,
		TaxOffice:    p.TaxOffice,
		VATID:        p.VATID,
		InvoiceEmail: p.InvoiceEmail,
		CreatedAt:    p.CreatedAt.UTC(),
		UpdatedAt:    p.UpdatedAt.UTC(),
	}
}

// PaymentMethod carries the currency the card is charged in, never the
// provider it is held by.
type PaymentMethod struct {
	ID        uuid.UUID `json:"id"`
	TeamID    uuid.UUID `json:"team_id"`
	Brand     string    `json:"brand" example:"visa"`
	Last4     string    `json:"last4" example:"4242"`
	ExpMonth  int       `json:"exp_month" example:"12"`
	ExpYear   int       `json:"exp_year" example:"2030"`
	IsDefault bool      `json:"is_default"`
	Currency  string    `json:"currency" example:"TRY"`
	CreatedAt time.Time `json:"created_at"`
}

func ToPaymentMethod(m *paymentmethod.PaymentMethod) *PaymentMethod {
	return &PaymentMethod{
		ID:        m.ID,
		TeamID:    m.TeamID,
		Brand:     m.Brand,
		Last4:     m.Last4,
		ExpMonth:  m.ExpMonth,
		ExpYear:   m.ExpYear,
		IsDefault: m.IsDefault,
		Currency:  m.Currency,
		CreatedAt: m.CreatedAt.UTC(),
	}
}

func ToPaymentMethods(methods paymentmethod.PaymentMethods) []*PaymentMethod {
	out := make([]*PaymentMethod, 0, len(methods))
	for _, m := range methods {
		out = append(out, ToPaymentMethod(m))
	}

	return out
}

// CardVerification is what the web app needs to open the hosted card form.
type CardVerification struct {
	VerificationID uuid.UUID `json:"verification_id"`
	RedirectURL    string    `json:"redirect_url"`
}

// BillingStatus is readable by every member, so it carries no amounts.
type BillingStatus struct {
	Status        string     `json:"status" enums:"no_payment_method,active,suspended"`
	OpenInvoiceID *uuid.UUID `json:"open_invoice_id"`
	NextRetryAt   *time.Time `json:"next_retry_at"`
	SuspendedAt   *time.Time `json:"suspended_at"`
	DeletionAt    *time.Time `json:"deletion_at"`
}

func ToBillingStatus(s *billingstatus.TeamStatus) *BillingStatus {
	return &BillingStatus{
		Status:        string(s.Status),
		OpenInvoiceID: s.OpenInvoiceID,
		NextRetryAt:   utc(s.NextRetryAt),
		SuspendedAt:   utc(s.SuspendedAt),
		DeletionAt:    utc(s.DeletionAt),
	}
}

// WebhookReceipt acknowledges a verified webhook.
type WebhookReceipt struct {
	Received bool `json:"received"`
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()

	return &u
}
