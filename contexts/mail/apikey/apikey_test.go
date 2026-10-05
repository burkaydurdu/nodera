package apikey_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/piggystats/nodera/contexts/mail/apikey"
	"github.com/piggystats/nodera/pkg/authz"
)

type memoryCache map[string]string

func (m memoryCache) GetString(_ context.Context, key string) (string, error) { return m[key], nil }

func (m memoryCache) Set(_ context.Context, key string, value any, _ time.Duration) error {
	m[key], _ = value.(string)

	return nil
}

const secret = "Q2xkS2VFVm5aV0Z1ZEhNeU1ESTJNVEF3TlRFeU1EQXdNREF3"

func store(t *testing.T, cache memoryCache, id string, permissions ...string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	sum := sha256.Sum256([]byte(secret))
	projectID, teamID := uuid.New(), uuid.New()
	raw, err := json.Marshal(map[string]any{"hash": hex.EncodeToString(sum[:]), "project_id": projectID, "team_id": teamID, "permissions": permissions})
	require.NoError(t, err)
	cache["api_key:"+id] = string(raw)

	return projectID, teamID
}

func TestVerify_AcceptsAStoredKeyWithThePermission(t *testing.T) {
	cache := memoryCache{}
	projectID, teamID := store(t, cache, "AbCdEf123456", "mail.send")
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

	key, err := apikey.NewVerifier(cache).WithClock(func() time.Time { return now }).Verify(context.Background(), "nd_AbCdEf123456."+secret, authz.MailSend)
	require.NoError(t, err)

	assert.Equal(t, "AbCdEf123456", key.ID)
	assert.Equal(t, projectID, key.ProjectID)
	assert.Equal(t, teamID, key.TeamID)
	assert.Equal(t, now.Format(time.RFC3339Nano), cache["api_key_last_used:AbCdEf123456"])
}

func TestVerify_RefusesWhatIsNotAValidKey(t *testing.T) {
	cache := memoryCache{}
	store(t, cache, "AbCdEf123456", "mail.send")
	verifier := apikey.NewVerifier(cache)

	for _, raw := range []string{
		"",
		"AbCdEf123456." + secret,
		"nd_AbCdEf123456",
		"nd_AbCdEf123456.wrong-secret",
		"nd_Unknown12345." + secret,
		"nd_short." + secret,
	} {
		_, err := verifier.Verify(context.Background(), raw, authz.MailSend)
		assert.ErrorIsf(t, err, apikey.ErrorUnauthorized, "%q", raw)
	}
}

func TestVerify_RefusesAKeyWithoutThePermission(t *testing.T) {
	cache := memoryCache{}
	store(t, cache, "AbCdEf123456", "droplet.read")

	_, err := apikey.NewVerifier(cache).Verify(context.Background(), "nd_AbCdEf123456."+secret, authz.MailSend)
	assert.ErrorIs(t, err, apikey.ErrorForbidden)
}

type brokenCache struct{}

func (brokenCache) GetString(context.Context, string) (string, error) { return "", assert.AnError }

func (brokenCache) Set(context.Context, string, any, time.Duration) error { return assert.AnError }

func TestVerify_AnUnreadableStoreIsNotTheCallersFault(t *testing.T) {
	_, err := apikey.NewVerifier(brokenCache{}).Verify(context.Background(), "nd_AbCdEf123456."+secret, authz.MailSend)
	assert.ErrorIs(t, err, apikey.ErrorUnavailable)
}
