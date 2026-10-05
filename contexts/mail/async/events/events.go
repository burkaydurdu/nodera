// Package events is the mail context's own view of the payloads it
// exchanges with the customer, billing and notification contexts: the same
// wire shape, declared here so the contexts never import each other
// (docs/adr/0004).
package events

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// --- customer → mail --------------------------------------------------------

type TeamMemberAddedEvent struct {
	Payload struct {
		TeamID  uuid.UUID `json:"team_id"`
		UserID  uuid.UUID `json:"user_id"`
		Role    string    `json:"role"`
		AddedAt time.Time `json:"added_at"`
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

// ProjectCreatedEvent carries the name since phase 5; a replay of an older
// event has none.
type ProjectCreatedEvent struct {
	Payload struct {
		ProjectID uuid.UUID `json:"project_id"`
		TeamID    uuid.UUID `json:"team_id"`
		Name      string    `json:"name"`
		CreatedAt time.Time `json:"created_at"`
	} `json:"payload"`
}

// --- billing → mail ---------------------------------------------------------

type TeamBillingStatusChangedEvent struct {
	Payload struct {
		TeamID    uuid.UUID `json:"team_id"`
		Status    string    `json:"status"`
		ChangedAt time.Time `json:"changed_at"`
	} `json:"payload"`
}

type TeamDropletsDeletionRequestedEvent struct {
	Payload struct {
		TeamID      uuid.UUID `json:"team_id"`
		RequestedAt time.Time `json:"requested_at"`
	} `json:"payload"`
}

// --- mail → billing ---------------------------------------------------------

// MailPackageStartedBody opens an interval [started_at, stopped_at) billing
// charges; the price is a two-decimal string, never a float.
type MailPackageStartedBody struct {
	ProjectID     uuid.UUID `json:"project_id"`
	TeamID        uuid.UUID `json:"team_id"`
	ProjectName   string    `json:"project_name"`
	PackageCode   string    `json:"package_code"`
	MonthlyEmails int       `json:"monthly_emails"`
	MonthlyPrice  string    `json:"monthly_price"`
	StartedAt     time.Time `json:"started_at"`
}

type MailPackageStartedEvent struct {
	Payload MailPackageStartedBody `json:"payload"`
}

// MailPackageStoppedBody names the interval it closes by its start, so it can
// arrive before the Started it closes.
type MailPackageStoppedBody struct {
	ProjectID uuid.UUID `json:"project_id"`
	TeamID    uuid.UUID `json:"team_id"`
	StartedAt time.Time `json:"started_at"`
	StoppedAt time.Time `json:"stopped_at"`
}

type MailPackageStoppedEvent struct {
	Payload MailPackageStoppedBody `json:"payload"`
}

// --- mail → notification ----------------------------------------------------
// Each names the Team and the Project and never a recipient (docs/adr/0008).

type MailDomainVerifiedBody struct {
	TeamID          uuid.UUID `json:"team_id"`
	ProjectID       uuid.UUID `json:"project_id"`
	ProjectName     string    `json:"project_name"`
	SendingDomainID uuid.UUID `json:"sending_domain_id"`
	Domain          string    `json:"domain"`
	VerifiedAt      time.Time `json:"verified_at"`
}

type MailDomainVerifiedEvent struct {
	Payload MailDomainVerifiedBody `json:"payload"`
}

type MailDomainUnverifiedBody struct {
	TeamID          uuid.UUID `json:"team_id"`
	ProjectID       uuid.UUID `json:"project_id"`
	ProjectName     string    `json:"project_name"`
	SendingDomainID uuid.UUID `json:"sending_domain_id"`
	Domain          string    `json:"domain"`
	UnverifiedAt    time.Time `json:"unverified_at"`
}

type MailDomainUnverifiedEvent struct {
	Payload MailDomainUnverifiedBody `json:"payload"`
}

// MailPackageUsageBody is the shape of both thresholds of an allowance.
type MailPackageUsageBody struct {
	TeamID      uuid.UUID `json:"team_id"`
	ProjectID   uuid.UUID `json:"project_id"`
	ProjectName string    `json:"project_name"`
	PackageCode string    `json:"package_code"`
	Allowance   int       `json:"allowance"`
	Used        int       `json:"used"`
	Period      string    `json:"period"`
	At          time.Time `json:"at"`
}

type MailPackageNearLimitEvent struct {
	Payload MailPackageUsageBody `json:"payload"`
}

type MailPackageExhaustedEvent struct {
	Payload MailPackageUsageBody `json:"payload"`
}

type MailRatesWarningBody struct {
	TeamID        uuid.UUID `json:"team_id"`
	ProjectID     uuid.UUID `json:"project_id"`
	ProjectName   string    `json:"project_name"`
	BounceRate    string    `json:"bounce_rate"`
	ComplaintRate string    `json:"complaint_rate"`
	WindowDays    int       `json:"window_days"`
	At            time.Time `json:"at"`
}

type MailRatesWarningEvent struct {
	Payload MailRatesWarningBody `json:"payload"`
}

type MailSendingPausedBody struct {
	TeamID        uuid.UUID `json:"team_id"`
	ProjectID     uuid.UUID `json:"project_id"`
	ProjectName   string    `json:"project_name"`
	Reason        string    `json:"reason"`
	BounceRate    string    `json:"bounce_rate"`
	ComplaintRate string    `json:"complaint_rate"`
	PausedAt      time.Time `json:"paused_at"`
}

type MailSendingPausedEvent struct {
	Payload MailSendingPausedBody `json:"payload"`
}

func ToJSONString(event any) (string, error) {
	data, err := json.Marshal(event)
	if err != nil {
		return "", err
	}

	return string(data), nil
}
