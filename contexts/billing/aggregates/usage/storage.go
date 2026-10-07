package usage

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// StorageInterval is one Package a Storage held, [StartedAt, StoppedAt), as
// the storage context reports it; StoppedAt is nil while the Storage holds
// it. The price was copied from the catalog when the Package was chosen, so
// a later catalog change never reprices it.
type StorageInterval struct {
	StorageID    uuid.UUID
	TeamID       uuid.UUID
	ProjectID    uuid.UUID
	ProjectName  string
	BucketName   string
	Region       string
	PackageCode  string
	SizeBytes    int64
	MonthlyPrice decimal.Decimal
	StartedAt    time.Time
	StoppedAt    *time.Time
}

// Holder keys Storage's intervals by the Storage, not its Project: a Project
// holds several Storages, each on a Package of its own.
func (i *StorageInterval) Holder() uuid.UUID { return i.StorageID }

func (i *StorageInterval) Price() decimal.Decimal { return i.MonthlyPrice }

func (i *StorageInterval) Span() (time.Time, *time.Time) { return i.StartedAt, i.StoppedAt }

// StorageLine is one Storage's line of an Invoice: the Storage and the
// Package it names and the amount the Storage still owes for the Period.
type StorageLine struct {
	StorageID   uuid.UUID
	BucketName  string
	ProjectID   uuid.UUID
	ProjectName string
	PackageCode string
	Amount      decimal.Decimal
}

// StorageLines is the Storage of a Period: one line per Storage that still
// owes for the Packages it held in [from, to) (see PackageCharges), carrying
// its Project's newest name.
func StorageLines(intervals []*StorageInterval, from, to time.Time, billed map[uuid.UUID]decimal.Decimal) []StorageLine {
	charges := PackageCharges(intervals, from, to, billed)
	lines := make([]StorageLine, 0, len(charges))
	for _, c := range charges {
		lines = append(lines, StorageLine{
			StorageID: c.Interval.StorageID, BucketName: c.Interval.BucketName, ProjectID: c.Interval.ProjectID,
			ProjectName: c.Interval.ProjectName, PackageCode: c.Interval.PackageCode, Amount: c.Amount,
		})
	}

	return lines
}
