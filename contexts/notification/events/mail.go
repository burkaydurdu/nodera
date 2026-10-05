package events

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Rate is a percentage as the mail context formatted it, with two decimals
// ("2.40" is 2.40%). json.Number takes the quoted string and refuses one that
// is no number, and the email shows the digits exactly as mail rounded them.
type Rate = json.Number

// Mail is the part every mail event shares: the Team and the Project it
// happened in, named the way mail knows it, so the Notification can name the
// Project without asking the customer context. Who in the Team is told is
// this context's decision (docs/adr/0008).
type Mail struct {
	TeamID      uuid.UUID `json:"team_id"`
	ProjectID   uuid.UUID `json:"project_id"`
	ProjectName string    `json:"project_name"`
}

type MailDomainVerifiedEvent struct {
	Payload struct {
		Mail
		SendingDomainID uuid.UUID `json:"sending_domain_id"`
		Domain          string    `json:"domain"`
		VerifiedAt      time.Time `json:"verified_at"`
	} `json:"payload"`
}

type MailDomainUnverifiedEvent struct {
	Payload struct {
		Mail
		SendingDomainID uuid.UUID `json:"sending_domain_id"`
		Domain          string    `json:"domain"`
		UnverifiedAt    time.Time `json:"unverified_at"`
	} `json:"payload"`
}

// MailPackageUsage is what MailPackageNearLimit (at 80% of the allowance) and
// MailPackageExhausted (at all of it) both carry. Period is the calendar
// month in UTC the allowance belongs to, "YYYY-MM".
type MailPackageUsage struct {
	Mail
	PackageCode string    `json:"package_code"`
	Allowance   int       `json:"allowance"`
	Used        int       `json:"used"`
	Period      string    `json:"period"`
	At          time.Time `json:"at"`
}

type MailPackageNearLimitEvent struct {
	Payload MailPackageUsage `json:"payload"`
}

type MailPackageExhaustedEvent struct {
	Payload MailPackageUsage `json:"payload"`
}

// MailRatesWarningEvent carries the rates over the window mail measured them
// on, in days.
type MailRatesWarningEvent struct {
	Payload struct {
		Mail
		BounceRate    Rate      `json:"bounce_rate"`
		ComplaintRate Rate      `json:"complaint_rate"`
		WindowDays    int       `json:"window_days"`
		At            time.Time `json:"at"`
	} `json:"payload"`
}

// MailSendingPausedEvent names the rate that crossed the line as a code,
// bounce_rate or complaint_rate, and carries both rates.
type MailSendingPausedEvent struct {
	Payload struct {
		Mail
		Reason        string    `json:"reason"`
		BounceRate    Rate      `json:"bounce_rate"`
		ComplaintRate Rate      `json:"complaint_rate"`
		PausedAt      time.Time `json:"paused_at"`
	} `json:"payload"`
}
