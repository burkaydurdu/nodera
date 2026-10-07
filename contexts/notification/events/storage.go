package events

import (
	"time"

	"github.com/google/uuid"
)

// Storage is the part every storage event shares: the Team, the Project and
// the Storage it happened to, named the way storage knows them, so the
// Notification can name the Project and the Bucket without asking another
// context. Who in the Team is told is this context's decision (docs/adr/0008).
type Storage struct {
	TeamID      uuid.UUID `json:"team_id"`
	ProjectID   uuid.UUID `json:"project_id"`
	ProjectName string    `json:"project_name"`
	StorageID   uuid.UUID `json:"storage_id"`
	BucketName  string    `json:"bucket_name"`
}

// StorageFill is what StorageNearlyFull (at 80% of the Package) and
// StorageFull (at all of it) both carry. A Storage can hold more than its
// Package for the minutes the engine takes to refuse writes, so UsedBytes may
// pass SizeBytes.
type StorageFill struct {
	Storage
	PackageCode string    `json:"package_code"`
	SizeBytes   int64     `json:"size_bytes"`
	UsedBytes   int64     `json:"used_bytes"`
	At          time.Time `json:"at"`
}

type StorageNearlyFullEvent struct {
	Payload StorageFill `json:"payload"`
}

type StorageFullEvent struct {
	Payload StorageFill `json:"payload"`
}

// StoragePausedEvent names why an operator Paused the Storage as a code:
// abuse_complaint, malware, phishing or legal.
type StoragePausedEvent struct {
	Payload struct {
		Storage
		Reason   string    `json:"reason"`
		PausedAt time.Time `json:"paused_at"`
	} `json:"payload"`
}

// StorageDowngradeSkippedEvent is a smaller Package that was due at a Period's
// start and not applied, because the Storage no longer fitted it. PackageCode
// is the Package the Storage stays on.
type StorageDowngradeSkippedEvent struct {
	Payload struct {
		Storage
		PackageCode        string    `json:"package_code"`
		SkippedPackageCode string    `json:"skipped_package_code"`
		SkippedSizeBytes   int64     `json:"skipped_size_bytes"`
		UsedBytes          int64     `json:"used_bytes"`
		At                 time.Time `json:"at"`
	} `json:"payload"`
}
