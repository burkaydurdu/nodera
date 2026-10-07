package storage_test

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"

	"github.com/piggystats/nodera/contexts/storage/aggregates/storage"
)

var at = time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)

func held(code string, size int64) storage.Package {
	return storage.Package{Code: code, SizeBytes: size, MonthlyPrice: decimal.NewFromInt(1), StartedAt: at}
}

func TestDecide_ALargerPackageIsAnUpgrade(t *testing.T) {
	assert.Equal(t, storage.ChangeUpgrade, storage.Decide(held("10gb", 10), nil, "50gb", 50))
}

func TestDecide_ASmallerPackageIsScheduled(t *testing.T) {
	assert.Equal(t, storage.ChangeScheduled, storage.Decide(held("50gb", 50), nil, "10gb", 10))
}

func TestDecide_TheCurrentPackageAgainUndoesASchedule(t *testing.T) {
	scheduled := &storage.Scheduled{Code: "10gb", SizeBytes: 10}
	assert.Equal(t, storage.ChangeUnscheduled, storage.Decide(held("50gb", 50), scheduled, "50gb", 50))
	assert.Equal(t, storage.ChangeNothing, storage.Decide(held("50gb", 50), nil, "50gb", 50))
}

func TestMeasure_TellsEachMarkOnceAndArmsItAgainBelow(t *testing.T) {
	s := &storage.Storage{Package: held("10gb", 1000)}

	nearly, full := s.Measure(799, 1, at)
	assert.False(t, nearly)
	assert.False(t, full)

	nearly, full = s.Measure(800, 1, at)
	assert.True(t, nearly)
	assert.False(t, full)

	nearly, _ = s.Measure(900, 1, at)
	assert.False(t, nearly, "already told")

	nearly, full = s.Measure(1000, 1, at)
	assert.False(t, nearly)
	assert.True(t, full)
	assert.Equal(t, storage.StateFull, s.State())

	s.Measure(100, 1, at)
	nearly, _ = s.Measure(850, 1, at)
	assert.True(t, nearly, "armed again under the mark")
}

func TestMeasure_StraightToFullIsToldOnce(t *testing.T) {
	s := &storage.Storage{Package: held("10gb", 1000)}

	nearly, full := s.Measure(1500, 1, at)
	assert.False(t, nearly)
	assert.True(t, full)
}

func TestHold_ALargerPackageArmsTheMarksTheStorageIsNowUnder(t *testing.T) {
	s := &storage.Storage{Package: held("10gb", 1000)}
	s.Measure(1000, 1, at)

	s.Hold(held("50gb", 5000))
	assert.Nil(t, s.NearlyFullNotifiedAt)
	assert.Nil(t, s.FullNotifiedAt)
	assert.Equal(t, storage.StateActive, s.State())
}

func TestState_TheFirstThingThatStopsTheStorage(t *testing.T) {
	s := &storage.Storage{Status: storage.StatusActive, Package: held("10gb", 1000), UsedBytes: 1000}
	assert.Equal(t, storage.StateFull, s.State())
	s.Pause = &storage.Pause{Reason: storage.ReasonLegal, At: at}
	assert.Equal(t, storage.StatePaused, s.State())
	s.TeamSuspended = true
	assert.Equal(t, storage.StateSuspended, s.State())
	s.Status = storage.StatusDeleting
	assert.Equal(t, storage.StateDeleting, s.State())
}

func TestStopped_PausedOrSuspended(t *testing.T) {
	s := &storage.Storage{}
	assert.False(t, s.Stopped())
	s.TeamSuspended = true
	assert.True(t, s.Stopped())
	s.TeamSuspended, s.Pause = false, &storage.Pause{}
	assert.True(t, s.Stopped())
}
