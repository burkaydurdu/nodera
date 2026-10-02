package droplet

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaimBillingEvents_NothingBeforeActive(t *testing.T) {
	d := &Droplet{Status: StatusProvisioning}

	assert.Empty(t, d.ClaimBillingEvents(time.Now()))
	assert.Nil(t, d.BillingStartedPublishedAt)
}

func TestClaimBillingEvents_StartedOnceWhenActive(t *testing.T) {
	now := time.Now()
	d := &Droplet{Status: StatusProvisioning}
	_, err := d.Transition(StatusActive, "provider reported active", now)
	require.NoError(t, err)

	first := d.ClaimBillingEvents(now)
	second := d.ClaimBillingEvents(now.Add(time.Minute))

	assert.Equal(t, []BillingEvent{BillingStarted}, first)
	assert.Empty(t, second, "a redelivered poll claims nothing")
	assert.Equal(t, now, *d.BillingStartedPublishedAt)
}

func TestClaimBillingEvents_StoppedAtDeletionRequest(t *testing.T) {
	now := time.Now()
	d := activeDroplet(t, now)
	require.Equal(t, []BillingEvent{BillingStarted}, d.ClaimBillingEvents(now))
	_, err := d.Transition(StatusDeleting, "deletion requested", now.Add(time.Hour))
	require.NoError(t, err)

	assert.Equal(t, []BillingEvent{BillingStopped}, d.ClaimBillingEvents(now.Add(time.Hour)))
	assert.Equal(t, now.Add(time.Hour), *d.BillingStoppedAt())
	assert.Empty(t, d.ClaimBillingEvents(now.Add(2*time.Hour)))
}

func TestClaimBillingEvents_StoppedAtFailureAfterActive(t *testing.T) {
	now := time.Now()
	d := activeDroplet(t, now)
	require.Equal(t, []BillingEvent{BillingStarted}, d.ClaimBillingEvents(now))
	_, err := d.Transition(StatusFailed, "server no longer exists at the provider", now.Add(time.Hour))
	require.NoError(t, err)

	assert.Equal(t, []BillingEvent{BillingStopped}, d.ClaimBillingEvents(now.Add(time.Hour)))
	assert.Equal(t, now.Add(time.Hour), *d.BillingStoppedAt())
}

func TestClaimBillingEvents_FailureBeforeActiveStopsNothing(t *testing.T) {
	now := time.Now()
	d := &Droplet{Status: StatusProvisioning}
	_, err := d.Transition(StatusFailed, "rejected", now)
	require.NoError(t, err)

	assert.Empty(t, d.ClaimBillingEvents(now))
	assert.Nil(t, d.BillingStoppedAt())
}

func TestClaimBillingEvents_BothAtOnceWhenNeverAnnounced(t *testing.T) {
	now := time.Now()
	d := activeDroplet(t, now)
	_, err := d.Transition(StatusDeleting, "deletion requested", now.Add(time.Hour))
	require.NoError(t, err)

	assert.Equal(t, []BillingEvent{BillingStarted, BillingStopped}, d.ClaimBillingEvents(now.Add(time.Hour)))
}

func TestBillingStoppedAt_FailureWinsOverALaterDeletion(t *testing.T) {
	now := time.Now()
	d := activeDroplet(t, now)
	_, err := d.Transition(StatusFailed, "gone", now.Add(time.Hour))
	require.NoError(t, err)
	_, err = d.Transition(StatusDeleting, "deletion requested", now.Add(2*time.Hour))
	require.NoError(t, err)

	assert.Equal(t, now.Add(time.Hour), *d.BillingStoppedAt(), "the meter stopped at the failure")
}

func TestMarkBillingPublished_RecordsEveryCrossedBoundary(t *testing.T) {
	now := time.Now()
	d := activeDroplet(t, now)

	d.MarkBillingPublished(now)

	assert.Equal(t, []BillingEvent{BillingStarted}, d.BillingEvents())
	assert.NotNil(t, d.BillingStartedPublishedAt)
	assert.Nil(t, d.BillingStoppedPublishedAt)
	assert.Empty(t, d.ClaimBillingEvents(now))
}

func activeDroplet(t *testing.T, now time.Time) *Droplet {
	t.Helper()
	d := &Droplet{Status: StatusProvisioning}
	_, err := d.Transition(StatusActive, "provider reported active", now)
	require.NoError(t, err)

	return d
}
