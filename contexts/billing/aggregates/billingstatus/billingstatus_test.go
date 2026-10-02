package billingstatus_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/piggystats/nodera/contexts/billing/aggregates/billingstatus"
)

func TestMove_TakesTheClockWhenItMovedOn(t *testing.T) {
	at := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	s := billingstatus.Default(uuid.New(), at.Add(-time.Hour))

	s.Move(billingstatus.Suspended, at)

	assert.Equal(t, billingstatus.Suspended, s.Status)
	assert.Equal(t, at, s.ChangedAt)
}

// Two changes in the same instant still reach the droplet context in order.
func TestMove_StaysAfterThePreviousChange(t *testing.T) {
	at := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	s := billingstatus.Default(uuid.New(), at)
	s.Move(billingstatus.Suspended, at)

	s.Move(billingstatus.Active, at)
	assert.Equal(t, at.Add(2*time.Microsecond), s.ChangedAt)

	s.Move(billingstatus.Suspended, at.Add(-time.Hour))
	assert.Equal(t, at.Add(3*time.Microsecond), s.ChangedAt, "a clock behind the row never moves it back")
}
