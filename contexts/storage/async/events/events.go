// Package events is the storage context's own view of the payloads it
// exchanges with the customer, billing and notification contexts: the same
// wire shape, declared here so the contexts never import each other
// (docs/adr/0004).
package events

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// --- customer → storage -----------------------------------------------------

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

type ProjectCreatedEvent struct {
	Payload struct {
		ProjectID uuid.UUID `json:"project_id"`
		TeamID    uuid.UUID `json:"team_id"`
		Name      string    `json:"name"`
		CreatedAt time.Time `json:"created_at"`
	} `json:"payload"`
}

// --- billing → storage ------------------------------------------------------

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

// --- storage → billing ------------------------------------------------------

// StoragePackageStartedBody opens an interval [started_at, stopped_at)
// billing charges for one Storage; the price is a two-decimal string, never a
// float.
type StoragePackageStartedBody struct {
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
}

type StoragePackageStartedEvent struct {
	Payload StoragePackageStartedBody `json:"payload"`
}

// StoragePackageStoppedBody names the interval it closes by its start, so it
// can arrive before the Started it closes.
type StoragePackageStoppedBody struct {
	StorageID uuid.UUID `json:"storage_id"`
	TeamID    uuid.UUID `json:"team_id"`
	StartedAt time.Time `json:"started_at"`
	StoppedAt time.Time `json:"stopped_at"`
}

type StoragePackageStoppedEvent struct {
	Payload StoragePackageStoppedBody `json:"payload"`
}

// --- storage → notification -------------------------------------------------
// Each names the Team, the Project and the Storage and never a recipient
// (docs/adr/0008).

// StorageFillBody is the shape of both marks of a Package.
type StorageFillBody struct {
	TeamID      uuid.UUID `json:"team_id"`
	ProjectID   uuid.UUID `json:"project_id"`
	ProjectName string    `json:"project_name"`
	StorageID   uuid.UUID `json:"storage_id"`
	BucketName  string    `json:"bucket_name"`
	PackageCode string    `json:"package_code"`
	SizeBytes   int64     `json:"size_bytes"`
	UsedBytes   int64     `json:"used_bytes"`
	At          time.Time `json:"at"`
}

type StorageNearlyFullEvent struct {
	Payload StorageFillBody `json:"payload"`
}

type StorageFullEvent struct {
	Payload StorageFillBody `json:"payload"`
}

type StoragePausedBody struct {
	TeamID      uuid.UUID `json:"team_id"`
	ProjectID   uuid.UUID `json:"project_id"`
	ProjectName string    `json:"project_name"`
	StorageID   uuid.UUID `json:"storage_id"`
	BucketName  string    `json:"bucket_name"`
	Reason      string    `json:"reason"`
	PausedAt    time.Time `json:"paused_at"`
}

type StoragePausedEvent struct {
	Payload StoragePausedBody `json:"payload"`
}

// StorageDowngradeSkippedBody is a smaller Package that was due at a Period's
// start and not applied, because the Storage no longer fitted it.
type StorageDowngradeSkippedBody struct {
	TeamID             uuid.UUID `json:"team_id"`
	ProjectID          uuid.UUID `json:"project_id"`
	ProjectName        string    `json:"project_name"`
	StorageID          uuid.UUID `json:"storage_id"`
	BucketName         string    `json:"bucket_name"`
	PackageCode        string    `json:"package_code"`
	SkippedPackageCode string    `json:"skipped_package_code"`
	SkippedSizeBytes   int64     `json:"skipped_size_bytes"`
	UsedBytes          int64     `json:"used_bytes"`
	At                 time.Time `json:"at"`
}

type StorageDowngradeSkippedEvent struct {
	Payload StorageDowngradeSkippedBody `json:"payload"`
}

func ToJSONString(event any) (string, error) {
	data, err := json.Marshal(event)
	if err != nil {
		return "", err
	}

	return string(data), nil
}
