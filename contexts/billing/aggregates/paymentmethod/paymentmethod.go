// Package paymentmethod is the card a Team saved for Nodera to charge: the
// Payment provider's references with brand, last four digits and expiry, and
// the hosted verification that produced it. Nodera never holds the number.
package paymentmethod

import (
	"time"

	"github.com/google/uuid"
)

// PaymentMethod keeps the provider and currency it was verified under so the
// provider never changes under a saved card (ADR-0006).
type PaymentMethod struct {
	ID                  uuid.UUID
	TeamID              uuid.UUID
	Provider            string
	Currency            string
	ProviderCustomerRef string
	ProviderCardRef     string
	Brand               string
	Last4               string
	ExpMonth            int
	ExpYear             int
	IsDefault           bool
	CreatedBy           uuid.UUID
	CreatedAt           time.Time
	DeletedAt           *time.Time
	// ExpiryNotifiedAt is when the expiry warning went out; it goes once.
	ExpiryNotifiedAt *time.Time
}

// ExpiresAt is the first instant the card no longer works: the start of the
// month after its expiry month. A card whose provider reported no expiry
// answers false.
func (m *PaymentMethod) ExpiresAt() (time.Time, bool) {
	if m.ExpMonth < 1 || m.ExpMonth > 12 || m.ExpYear <= 0 {
		return time.Time{}, false
	}

	return time.Date(m.ExpYear, time.Month(m.ExpMonth)+1, 1, 0, 0, 0, 0, time.UTC), true
}

// ExpiresWithin reports a card still valid at now that stops working within
// the window.
func (m *PaymentMethod) ExpiresWithin(now time.Time, window time.Duration) bool {
	at, ok := m.ExpiresAt()
	if !ok {
		return false
	}

	return now.Before(at) && !at.After(now.Add(window))
}

type PaymentMethods []*PaymentMethod

// Default answers the default card, nil when the Team has none.
func (p PaymentMethods) Default() *PaymentMethod {
	for _, m := range p {
		if m.IsDefault {
			return m
		}
	}

	return nil
}

type VerificationStatus string

const (
	VerificationPending   VerificationStatus = "pending"
	VerificationCompleted VerificationStatus = "completed"
	VerificationFailed    VerificationStatus = "failed"
)

// Verification is one hosted card verification started for a Team; the
// provider's reference is what the completion asks the provider about.
type Verification struct {
	ID              uuid.UUID
	TeamID          uuid.UUID
	Provider        string
	Currency        string
	ProviderRef     string
	Status          VerificationStatus
	PaymentMethodID *uuid.UUID
	FailureReason   string
	CreatedBy       uuid.UUID
	CreatedAt       time.Time
	CompletedAt     *time.Time
}

// Route is one row of the country → Payment provider table; '*' is the
// default row.
type Route struct {
	Country  string
	Provider string
	Currency string
}

const RouteDefault = "*"
