// Package droplet is what a customer buys: a bare-metal server rented from a
// Provider at a price locked when it was created, with the history of every
// status it went through.
package droplet

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type Status string

const (
	StatusProvisioning Status = "provisioning"
	StatusActive       Status = "active"
	StatusOff          Status = "off"
	StatusFailed       Status = "failed"
	StatusDeleting     Status = "deleting"
	StatusDeleted      Status = "deleted"
)

// Action is a power request a customer makes; the resulting status comes
// from the poller, not from the request.
type Action string

const (
	ActionPowerOn  Action = "power_on"
	ActionPowerOff Action = "power_off"
	ActionReboot   Action = "reboot"
)

// SSHKey is what a Droplet remembers of the keys it was created with; the
// public key text itself belongs to the customer context.
type SSHKey struct {
	Name        string `json:"name"`
	Fingerprint string `json:"fingerprint"`
}

type Droplet struct {
	ID              uuid.UUID
	ProjectID       uuid.UUID
	TeamID          uuid.UUID
	PlanID          uuid.UUID
	PlanCode        string
	RegionCode      string
	ImageCode       string
	Hostname        string
	HourlyPrice     decimal.Decimal
	MonthlyCapHours int
	Status          Status
	ProviderID      uuid.UUID
	ProviderRef     *string
	IPv4            *string
	IPv6            *string
	SSHKeys         []SSHKey
	PendingAction   *Action
	FailureReason   *string
	CreatedBy       uuid.UUID
	CreatedAt       time.Time
	UpdatedAt       time.Time
	ActiveAt        *time.Time
	DeletionRequest *time.Time
	DeletedAt       *time.Time
	FailedAt        *time.Time

	// The meter's boundaries announced so far (see billing.go) and the mark
	// of a Droplet this context powered off because its Team was suspended.
	BillingStartedPublishedAt *time.Time
	BillingStoppedPublishedAt *time.Time
	SuspendedPowerOffAt       *time.Time
}

type Droplets []*Droplet

// HistoryEntry is one transition; From is nil for the creating one.
type HistoryEntry struct {
	From   *Status
	To     Status
	At     time.Time
	Reason string
}

type History []*HistoryEntry
