package droplet

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatus_CanTransitionTo(t *testing.T) {
	cases := []struct {
		from, to Status
		allowed  bool
	}{
		{StatusProvisioning, StatusActive, true},
		{StatusProvisioning, StatusFailed, true},
		{StatusProvisioning, StatusDeleting, true},
		{StatusProvisioning, StatusDeleted, false},
		{StatusActive, StatusOff, true},
		{StatusActive, StatusDeleting, true},
		{StatusActive, StatusProvisioning, false},
		{StatusOff, StatusActive, true},
		{StatusFailed, StatusDeleting, true},
		{StatusFailed, StatusActive, false},
		{StatusDeleting, StatusDeleted, true},
		{StatusDeleting, StatusActive, false},
		{StatusDeleted, StatusDeleting, false},
	}
	for _, tc := range cases {
		t.Run(string(tc.from)+"->"+string(tc.to), func(t *testing.T) {
			assert.Equal(t, tc.allowed, tc.from.CanTransitionTo(tc.to))
		})
	}
}

func TestStatus_AllowsPowerAction_OnlyLiveStatuses(t *testing.T) {
	assert.True(t, StatusActive.AllowsPowerAction())
	assert.True(t, StatusOff.AllowsPowerAction())
	assert.False(t, StatusProvisioning.AllowsPowerAction())
	assert.False(t, StatusFailed.AllowsPowerAction())
	assert.False(t, StatusDeleting.AllowsPowerAction())
	assert.False(t, StatusDeleted.AllowsPowerAction())
}

func TestStatus_AllowsDelete_NeverTwice(t *testing.T) {
	assert.True(t, StatusProvisioning.AllowsDelete())
	assert.True(t, StatusActive.AllowsDelete())
	assert.True(t, StatusOff.AllowsDelete())
	assert.True(t, StatusFailed.AllowsDelete())
	assert.False(t, StatusDeleting.AllowsDelete())
	assert.False(t, StatusDeleted.AllowsDelete())
}

func TestTransition_FirstActiveStampsActiveAtOnce(t *testing.T) {
	d := &Droplet{Status: StatusProvisioning}
	first := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)

	entry, err := d.Transition(StatusActive, "provider reported active", first)
	require.NoError(t, err)
	assert.Equal(t, StatusProvisioning, *entry.From)
	assert.Equal(t, StatusActive, entry.To)
	require.NotNil(t, d.ActiveAt)
	assert.Equal(t, first, *d.ActiveAt)

	_, err = d.Transition(StatusOff, "", first.Add(time.Hour))
	require.NoError(t, err)
	_, err = d.Transition(StatusActive, "", first.Add(2*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, first, *d.ActiveAt, "active_at is the first activation only")
}

func TestTransition_DeletingStampsTheRequestAndDeletedTheRelease(t *testing.T) {
	d := &Droplet{Status: StatusActive}
	requested := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)

	_, err := d.Transition(StatusDeleting, "customer asked", requested)
	require.NoError(t, err)
	require.NotNil(t, d.DeletionRequest)
	assert.Nil(t, d.DeletedAt)

	_, err = d.Transition(StatusDeleted, "provider released", requested.Add(time.Minute))
	require.NoError(t, err)
	assert.Equal(t, requested, *d.DeletionRequest, "the meter stops at the request, not the release")
	assert.NotNil(t, d.DeletedAt)
}

func TestTransition_FailedKeepsTheReason(t *testing.T) {
	d := &Droplet{Status: StatusProvisioning}
	_, err := d.Transition(StatusFailed, "the server request was refused", time.Now())
	require.NoError(t, err)
	require.NotNil(t, d.FailureReason)
	assert.Equal(t, "the server request was refused", *d.FailureReason)
}

func TestTransition_RefusedMoveLeavesTheDropletUntouched(t *testing.T) {
	d := &Droplet{Status: StatusDeleted}
	_, err := d.Transition(StatusActive, "", time.Now())
	assert.ErrorIs(t, err, ErrorInvalidTransition)
	assert.Equal(t, StatusDeleted, d.Status)
}

func TestAction_TargetStatus(t *testing.T) {
	assert.Equal(t, StatusOff, ActionPowerOff.TargetStatus())
	assert.Equal(t, StatusActive, ActionPowerOn.TargetStatus())
	assert.Equal(t, StatusActive, ActionReboot.TargetStatus())
}
