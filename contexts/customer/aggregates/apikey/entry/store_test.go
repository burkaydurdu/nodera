package entry_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/piggystats/nodera/contexts/customer/aggregates/apikey"
	"github.com/piggystats/nodera/contexts/customer/aggregates/apikey/entry"
	"github.com/piggystats/nodera/pkg/authz"
)

// fakeCache is the three Redis calls the store makes, over a map that also
// remembers each key's expiry.
type fakeCache struct {
	values map[string]string
	ttls   map[string]time.Duration
}

func newFakeCache() *fakeCache {
	return &fakeCache{values: map[string]string{}, ttls: map[string]time.Duration{}}
}

func (c *fakeCache) Set(_ context.Context, key string, value any, expiration time.Duration) error {
	c.values[key] = fmt.Sprint(value)
	c.ttls[key] = expiration

	return nil
}

func (c *fakeCache) GetString(_ context.Context, key string) (string, error) {
	return c.values[key], nil
}

func (c *fakeCache) Delete(_ context.Context, keys ...string) error {
	for _, key := range keys {
		delete(c.values, key)
	}

	return nil
}

func TestWrite_StoresTheContractRecordWithoutExpiry(t *testing.T) {
	cache := newFakeCache()
	key := &apikey.APIKey{
		ID:          "AbCdEf012345",
		ProjectID:   uuid.MustParse("019a0000-0000-7000-8000-0000000000a1"),
		TeamID:      uuid.MustParse("019a0000-0000-7000-8000-0000000000b2"),
		Name:        "production",
		SecretHash:  apikey.HashSecret("secret"),
		Permissions: []authz.Permission{authz.MailSend},
	}

	require.NoError(t, entry.NewStore(cache).Write(context.Background(), key))

	assert.JSONEq(t, fmt.Sprintf(
		`{"hash":%q,"project_id":"019a0000-0000-7000-8000-0000000000a1","team_id":"019a0000-0000-7000-8000-0000000000b2","permissions":["mail.send"]}`,
		apikey.HashSecret("secret"),
	), cache.values["api_key:AbCdEf012345"])
	assert.Equal(t, time.Duration(0), cache.ttls["api_key:AbCdEf012345"], "a key lives until it is revoked")
}

func TestDelete_RemovesTheRecord(t *testing.T) {
	cache := newFakeCache()
	cache.values["api_key:AbCdEf012345"] = "{}"
	cache.values["api_key:Other0123456"] = "{}"

	require.NoError(t, entry.NewStore(cache).Delete(context.Background(), "AbCdEf012345"))

	assert.NotContains(t, cache.values, "api_key:AbCdEf012345")
	assert.Contains(t, cache.values, "api_key:Other0123456")
}

func TestLastUsed_ReadsWhatTheAuthenticatorsWroteInUTC(t *testing.T) {
	cache := newFakeCache()
	cache.values["api_key_last_used:AbCdEf012345"] = "2026-10-05T09:30:00.123456789Z"
	cache.values["api_key_last_used:Plain0123456"] = "2026-10-05T12:30:00+03:00"

	used, err := entry.NewStore(cache).LastUsed(context.Background(), []string{"AbCdEf012345", "Plain0123456"})

	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 10, 5, 9, 30, 0, 123456789, time.UTC), used["AbCdEf012345"])
	assert.Equal(t, time.Date(2026, 10, 5, 9, 30, 0, 0, time.UTC), used["Plain0123456"])
	assert.Equal(t, time.UTC, used["Plain0123456"].Location())
}

func TestLastUsed_NeverUsedOrUnreadableIsAbsent(t *testing.T) {
	cache := newFakeCache()
	cache.values["api_key_last_used:Garbage01234"] = "yesterday"

	used, err := entry.NewStore(cache).LastUsed(context.Background(), []string{"Unused012345", "Garbage01234"})

	require.NoError(t, err)
	assert.Empty(t, used)
}
