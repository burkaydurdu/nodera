// Package dto is the storage context's wire shapes: what the web app is
// answered.
package dto

import (
	"time"

	"github.com/google/uuid"
)

type Region struct {
	Code    string `json:"code"`
	City    string `json:"city"`
	Country string `json:"country"`
}

type Package struct {
	Code         string `json:"code"`
	SizeBytes    int64  `json:"size_bytes"`
	MonthlyPrice string `json:"monthly_price"`
}

type HeldPackage struct {
	Code         string    `json:"code"`
	SizeBytes    int64     `json:"size_bytes"`
	MonthlyPrice string    `json:"monthly_price"`
	StartedAt    time.Time `json:"started_at"`
}

type ScheduledChange struct {
	Code         string    `json:"code"`
	SizeBytes    int64     `json:"size_bytes"`
	MonthlyPrice string    `json:"monthly_price"`
	EffectiveAt  time.Time `json:"effective_at"`
}

type Pause struct {
	Reason   string    `json:"reason"`
	PausedAt time.Time `json:"paused_at"`
}

// Storage is one Storage as the web app shows it. State is the first thing
// stopping it right now: deleting, suspended, paused, full or active.
// Endpoint is what S3 tools are configured with and URL where the bucket
// itself answers.
type Storage struct {
	ID              uuid.UUID        `json:"id"`
	ProjectID       uuid.UUID        `json:"project_id"`
	BucketName      string           `json:"bucket_name"`
	Region          string           `json:"region"`
	Endpoint        string           `json:"endpoint"`
	URL             string           `json:"url"`
	Status          string           `json:"status" enums:"active,deleting"`
	State           string           `json:"state" enums:"deleting,suspended,paused,full,active"`
	Package         HeldPackage      `json:"package"`
	ScheduledChange *ScheduledChange `json:"scheduled_change"`
	UsedBytes       int64            `json:"used_bytes"`
	ObjectCount     int64            `json:"object_count"`
	MeasuredAt      *time.Time       `json:"measured_at"`
	Pause           *Pause           `json:"pause"`
	BillingStatus   string           `json:"billing_status"`
	CreatedAt       time.Time        `json:"created_at"`
}

type AccessKey struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	AccessKeyID string    `json:"access_key_id"`
	Permission  string    `json:"permission" enums:"read,read_write"`
	CreatedBy   uuid.UUID `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
}

// CreatedAccessKey carries the secret key, the one time it is shown.
type CreatedAccessKey struct {
	AccessKey
	SecretKey string `json:"secret_key"`
}

type Object struct {
	Key        string    `json:"key"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modified_at"`
}

// Listing is one page of a folder; NextCursor is empty on the last page.
type Listing struct {
	Prefix     string   `json:"prefix"`
	Folders    []string `json:"folders"`
	Objects    []Object `json:"objects"`
	NextCursor *string  `json:"next_cursor"`
}

// PresignedURL is what the browser sends. ContentType, when set, was signed
// into the URL and must go with the request as its Content-Type exactly.
type PresignedURL struct {
	URL         string    `json:"url"`
	Method      string    `json:"method"`
	ContentType string    `json:"content_type,omitempty"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// ObjectAccess is whether anyone may read an object without a key; writing
// always needs one.
type ObjectAccess struct {
	Key    string `json:"key"`
	URL    string `json:"url"`
	Public bool   `json:"public"`
}

type UsageDay struct {
	Day           string `json:"day"`
	UsedBytes     int64  `json:"used_bytes"`
	ObjectCount   int64  `json:"object_count"`
	BytesSent     int64  `json:"bytes_sent"`
	BytesReceived int64  `json:"bytes_received"`
	Requests      int64  `json:"requests"`
}

type Usage struct {
	StorageID uuid.UUID  `json:"storage_id"`
	Days      []UsageDay `json:"days"`
}
