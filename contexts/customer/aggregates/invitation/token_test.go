package invitation_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/piggystats/nodera/contexts/customer/aggregates/invitation"
)

func TestNewToken_IsBase64URLOf32BytesAndHashesToSHA256Hex(t *testing.T) {
	raw, tokenHash, err := invitation.NewToken()

	require.NoError(t, err)
	assert.Len(t, raw, 43)
	assert.NotContains(t, raw, "=")
	assert.NotContains(t, raw, "+")
	assert.NotContains(t, raw, "/")
	assert.Len(t, tokenHash, 64)
	assert.Equal(t, tokenHash, invitation.HashToken(raw))
}

func TestNewToken_NeverRepeats(t *testing.T) {
	first, _, err := invitation.NewToken()
	require.NoError(t, err)
	second, _, err := invitation.NewToken()
	require.NoError(t, err)

	assert.NotEqual(t, first, second)
}

func TestMaskEmail(t *testing.T) {
	cases := map[string]string{
		"burkay@example.com": "b***@example.com",
		"a@b.co":             "a***@b.co",
		"no-at-sign":         "***",
		"@example.com":       "***",
	}
	for in, want := range cases {
		assert.Equal(t, want, invitation.MaskEmail(in), in)
	}
}

func TestEffectiveStatus_PendingPastItsDate_ReadsAsExpired(t *testing.T) {
	now := time.Now()
	inv := &invitation.Invitation{Status: invitation.StatusPending, ExpiresAt: now.Add(-time.Second)}

	assert.Equal(t, invitation.StatusExpired, inv.EffectiveStatus(now))
	assert.False(t, inv.IsPending(now))
}

func TestEffectiveStatus_PendingBeforeItsDate_StaysPending(t *testing.T) {
	now := time.Now()
	inv := &invitation.Invitation{Status: invitation.StatusPending, ExpiresAt: now.Add(time.Hour)}

	assert.Equal(t, invitation.StatusPending, inv.EffectiveStatus(now))
	assert.True(t, inv.IsPending(now))
}

func TestEffectiveStatus_AnsweredOffer_IgnoresTheDate(t *testing.T) {
	now := time.Now()
	inv := &invitation.Invitation{Status: invitation.StatusDeclined, ExpiresAt: now.Add(-time.Hour)}

	assert.Equal(t, invitation.StatusDeclined, inv.EffectiveStatus(now))
}

func TestResendAvailableIn_CountsDownFromTheLastSend(t *testing.T) {
	now := time.Now()
	inv := &invitation.Invitation{LastSentAt: now.Add(-20 * time.Second)}

	assert.InDelta(t, 40, inv.ResendAvailableIn(now).Seconds(), 0.01)
	assert.LessOrEqual(t, inv.ResendAvailableIn(now.Add(time.Minute)), time.Duration(0))
}
