// Package dto is the mail context's wire shapes: what the web app and the
// sending API answer.
package dto

import (
	"time"

	"github.com/google/uuid"
)

type Package struct {
	Code          string `json:"code"`
	MonthlyEmails int    `json:"monthly_emails"`
	MonthlyPrice  string `json:"monthly_price"`
}

type HeldPackage struct {
	Code          string    `json:"code"`
	MonthlyEmails int       `json:"monthly_emails"`
	MonthlyPrice  string    `json:"monthly_price"`
	StartedAt     time.Time `json:"started_at"`
}

type ScheduledChange struct {
	// Code is a Package, or "none" for a cancellation.
	Code        string    `json:"code"`
	EffectiveAt time.Time `json:"effective_at"`
}

type Period struct {
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	Allowance int       `json:"allowance"`
	Used      int       `json:"used"`
}

type DailyLimit struct {
	Limit    int       `json:"limit"`
	Used     int       `json:"used"`
	ResetsAt time.Time `json:"resets_at"`
}

type Rates struct {
	WindowDays    int    `json:"window_days"`
	Sent          int    `json:"sent"`
	BounceRate    string `json:"bounce_rate"`
	ComplaintRate string `json:"complaint_rate"`
}

type Pause struct {
	Reason   string    `json:"reason"`
	PausedAt time.Time `json:"paused_at"`
}

// Overview is a Project's Mail at a glance; SendingState says in one word
// whether a Message would be accepted right now.
type Overview struct {
	ProjectID       uuid.UUID        `json:"project_id"`
	BillingStatus   string           `json:"billing_status"`
	Package         *HeldPackage     `json:"package"`
	ScheduledChange *ScheduledChange `json:"scheduled_change"`
	Period          *Period          `json:"period"`
	DailyLimit      DailyLimit       `json:"daily_limit"`
	Rates           Rates            `json:"rates"`
	Pause           *Pause           `json:"pause"`
	SendingState    string           `json:"sending_state"`
}

type Record struct {
	Purpose   string     `json:"purpose"`
	Type      string     `json:"type"`
	Host      string     `json:"host"`
	Value     string     `json:"value"`
	Valid     bool       `json:"valid"`
	CheckedAt *time.Time `json:"checked_at"`
}

type SendingDomain struct {
	ID            uuid.UUID  `json:"id"`
	Name          string     `json:"name"`
	Status        string     `json:"status"`
	DMARCFound    bool       `json:"dmarc_found"`
	Records       []Record   `json:"records"`
	VerifiedAt    *time.Time `json:"verified_at"`
	LastCheckedAt *time.Time `json:"last_checked_at"`
	CreatedAt     time.Time  `json:"created_at"`
}

type RecipientSummary struct {
	Address string `json:"address"`
	Status  string `json:"status"`
}

type MessageSummary struct {
	ID         uuid.UUID          `json:"id"`
	From       string             `json:"from"`
	Subject    string             `json:"subject"`
	Recipients []RecipientSummary `json:"recipients"`
	Tags       []string           `json:"tags"`
	Source     string             `json:"source"`
	CreatedAt  time.Time          `json:"created_at"`
}

type MessagePage struct {
	Items      []MessageSummary `json:"items"`
	NextCursor *string          `json:"next_cursor"`
}

type Response struct {
	Code         int    `json:"code"`
	EnhancedCode string `json:"enhanced_code"`
	Text         string `json:"text"`
}

type Event struct {
	Type       string    `json:"type"`
	OccurredAt time.Time `json:"occurred_at"`
	Response   *Response `json:"response"`
}

type RecipientDetail struct {
	Address string  `json:"address"`
	Kind    string  `json:"kind"`
	Status  string  `json:"status"`
	Events  []Event `json:"events"`
}

type MessageDetail struct {
	ID              uuid.UUID         `json:"id"`
	From            string            `json:"from"`
	Subject         string            `json:"subject"`
	MessageIDHeader string            `json:"message_id_header"`
	Tags            []string          `json:"tags"`
	Source          string            `json:"source"`
	SizeBytes       int               `json:"size_bytes"`
	CreatedAt       time.Time         `json:"created_at"`
	Recipients      []RecipientDetail `json:"recipients"`
}

type Suppression struct {
	ID        uuid.UUID  `json:"id"`
	Address   string     `json:"address"`
	Reason    string     `json:"reason"`
	MessageID *uuid.UUID `json:"message_id"`
	CreatedAt time.Time  `json:"created_at"`
}

type SuppressionPage struct {
	Items      []Suppression `json:"items"`
	NextCursor *string       `json:"next_cursor"`
}

type Webhook struct {
	ID        uuid.UUID `json:"id"`
	URL       string    `json:"url"`
	Events    []string  `json:"events"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
	// Secret is answered once, when the Webhook is created.
	Secret string `json:"secret,omitempty"`
}

type Delivery struct {
	ID             uuid.UUID  `json:"id"`
	Status         string     `json:"status"`
	Attempts       int        `json:"attempts"`
	EventCount     int        `json:"event_count"`
	LastStatusCode *int       `json:"last_status_code"`
	LastError      *string    `json:"last_error"`
	NextAttemptAt  *time.Time `json:"next_attempt_at"`
	CreatedAt      time.Time  `json:"created_at"`
	LastAttemptAt  *time.Time `json:"last_attempt_at"`
}

type DeliveryPage struct {
	Items      []Delivery `json:"items"`
	NextCursor *string    `json:"next_cursor"`
}

// Sent is the sending API's answer.
type Sent struct {
	ID         uuid.UUID `json:"id"`
	Accepted   int       `json:"accepted"`
	Suppressed []string  `json:"suppressed"`
}

// DKIMKey is what KumoMTA signs a Message with; it never leaves the internal
// listener.
type DKIMKey struct {
	Domain        string `json:"domain"`
	Selector      string `json:"selector"`
	PrivateKeyPEM string `json:"private_key_pem"`
}
