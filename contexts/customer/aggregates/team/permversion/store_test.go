package permversion_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/piggystats/nodera/contexts/customer/aggregates/team/permversion"
)

// fakeCache is the two Redis calls the store makes, over a map.
type fakeCache struct{ values map[string]string }

func (c *fakeCache) GetString(_ context.Context, key string) (string, error) {
	return c.values[key], nil
}

func (c *fakeCache) Update(_ context.Context, key string, mutate func(string) (string, time.Duration, bool, error)) error {
	next, _, skip, err := mutate(c.values[key])
	if err != nil || skip {
		return err
	}
	c.values[key] = next

	return nil
}

func TestCurrent_MissingKey_IsZero(t *testing.T) {
	store := permversion.NewStore(&fakeCache{values: map[string]string{}})

	version, err := store.Current(context.Background(), uuid.New())

	require.NoError(t, err)
	assert.Equal(t, int64(0), version)
}

func TestBump_FromMissing_IsOne(t *testing.T) {
	cache := &fakeCache{values: map[string]string{}}
	store := permversion.NewStore(cache)
	userID := uuid.New()

	require.NoError(t, store.Bump(context.Background(), userID))

	version, err := store.Current(context.Background(), userID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), version)
	assert.Equal(t, "1", cache.values["user_perm_version:"+userID.String()], "a plain integer string, as the gateway reads it")
}

func TestBump_Increments(t *testing.T) {
	userID := uuid.New()
	cache := &fakeCache{values: map[string]string{"user_perm_version:" + userID.String(): "41"}}
	store := permversion.NewStore(cache)

	require.NoError(t, store.Bump(context.Background(), userID))

	version, err := store.Current(context.Background(), userID)
	require.NoError(t, err)
	assert.Equal(t, int64(42), version)
}

func TestCurrent_GarbageValue_ReadsAsZero(t *testing.T) {
	userID := uuid.New()
	store := permversion.NewStore(&fakeCache{values: map[string]string{"user_perm_version:" + userID.String(): "not-a-number"}})

	version, err := store.Current(context.Background(), userID)

	require.NoError(t, err)
	assert.Equal(t, int64(0), version)
}
