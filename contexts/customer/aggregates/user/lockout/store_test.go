package lockout

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCache is an in-memory stand-in for the Redis wrapper: the store's
// behaviour is what is under test, not the transport.
type fakeCache struct {
	values map[string]string
}

func newFakeCache() *fakeCache { return &fakeCache{values: map[string]string{}} }

func (f *fakeCache) GetString(_ context.Context, key string) (string, error) {
	return f.values[key], nil
}

func (f *fakeCache) Delete(_ context.Context, keys ...string) error {
	for _, k := range keys {
		delete(f.values, k)
	}

	return nil
}

func (f *fakeCache) Update(_ context.Context, key string, mutate func(string) (string, time.Duration, bool, error)) error {
	next, _, skip, err := mutate(f.values[key])
	if err != nil {
		return err
	}
	if !skip {
		f.values[key] = next
	}

	return nil
}

func TestStore_FifthFailureLocksForOneMinute(t *testing.T) {
	store := NewStore(newFakeCache())
	ctx := context.Background()

	for i := 0; i < 4; i++ {
		justLocked, _, err := store.RegisterFailure(ctx, "a@example.com")
		require.NoError(t, err)
		assert.False(t, justLocked, "miss %d must not lock", i+1)
	}

	justLocked, retryAfter, err := store.RegisterFailure(ctx, "a@example.com")
	require.NoError(t, err)
	assert.True(t, justLocked)
	assert.Equal(t, LockSchedule[0], retryAfter)

	locked, remaining := store.Status(ctx, "A@Example.com ")
	assert.True(t, locked, "the email is matched case- and space-insensitively")
	assert.Greater(t, remaining, 50*time.Second)
}

func TestStore_SecondLockNeedsThreeMissesAndLastsFiveMinutes(t *testing.T) {
	cache := newFakeCache()
	store := NewStore(cache)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_, _, _ = store.RegisterFailure(ctx, "a@example.com")
	}
	// Let the first lock lapse while keeping the escalation level.
	cache.values[key("a@example.com")] = `{"fail_count":0,"lock_level":1,"locked_until":"2000-01-01T00:00:00Z"}`

	var justLocked bool
	var retryAfter time.Duration
	for i := 0; i < 3; i++ {
		justLocked, retryAfter, _ = store.RegisterFailure(ctx, "a@example.com")
	}

	assert.True(t, justLocked)
	assert.Equal(t, LockSchedule[1], retryAfter)
}

func TestStore_FailureWhileLocked_DoesNotEscalate(t *testing.T) {
	store := NewStore(newFakeCache())
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		_, _, _ = store.RegisterFailure(ctx, "a@example.com")
	}

	justLocked, retryAfter, err := store.RegisterFailure(ctx, "a@example.com")

	require.NoError(t, err)
	assert.False(t, justLocked)
	assert.Greater(t, retryAfter, time.Duration(0))
}

func TestStore_ResetClearsTheLock(t *testing.T) {
	store := NewStore(newFakeCache())
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		_, _, _ = store.RegisterFailure(ctx, "a@example.com")
	}

	require.NoError(t, store.Reset(ctx, "a@example.com"))

	locked, _ := store.Status(ctx, "a@example.com")
	assert.False(t, locked)
}

func TestStore_UnknownEmailIsNotLocked(t *testing.T) {
	store := NewStore(newFakeCache())

	locked, remaining := store.Status(context.Background(), "nobody@example.com")

	assert.False(t, locked)
	assert.Equal(t, time.Duration(0), remaining)
}
