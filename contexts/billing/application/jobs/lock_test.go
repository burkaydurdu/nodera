package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// memoryCache is the slice of Redis the Locker needs, in a map: it lets the
// tests replace the stored token underneath a running holder.
type memoryCache struct {
	values map[string]string
	setErr error
}

func newMemoryCache() *memoryCache { return &memoryCache{values: make(map[string]string)} }

func (c *memoryCache) SetNX(_ context.Context, key string, value any, _ time.Duration) (bool, error) {
	if c.setErr != nil {
		return false, c.setErr
	}
	if _, taken := c.values[key]; taken {
		return false, nil
	}
	c.values[key] = value.(string)

	return true, nil
}

func (c *memoryCache) CompareAndDelete(_ context.Context, key, value string) (bool, error) {
	if c.values[key] != value {
		return false, nil
	}
	delete(c.values, key)

	return true, nil
}

func TestWithLock_RunsAndReleases(t *testing.T) {
	cache := newMemoryCache()
	ran := false

	ok, err := NewLocker(cache).WithLock(context.Background(), "k", time.Minute, func(context.Context) error {
		ran = true
		_, held := cache.values["k"]
		assert.True(t, held, "held while running")

		return nil
	})

	require.NoError(t, err)
	assert.True(t, ok)
	assert.True(t, ran)
	assert.Empty(t, cache.values, "released afterwards")
}

func TestWithLock_SkipsWhenTaken(t *testing.T) {
	cache := newMemoryCache()
	cache.values["k"] = "other"

	ok, err := NewLocker(cache).WithLock(context.Background(), "k", time.Minute, func(context.Context) error {
		t.Fatal("must not run")

		return nil
	})

	require.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, "other", cache.values["k"], "the holder's lock is untouched")
}

func TestWithLock_ReleaseKeepsAnotherHoldersToken(t *testing.T) {
	cache := newMemoryCache()

	ok, err := NewLocker(cache).WithLock(context.Background(), "k", time.Minute, func(context.Context) error {
		// The lease expired mid-run and another replica took the lock.
		cache.values["k"] = "other"

		return nil
	})

	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "other", cache.values["k"])
}

func TestWithLock_AnswersTheRunError(t *testing.T) {
	cache := newMemoryCache()
	boom := errors.New("boom")

	ok, err := NewLocker(cache).WithLock(context.Background(), "k", time.Minute, func(context.Context) error { return boom })

	assert.True(t, ok)
	assert.ErrorIs(t, err, boom)
	assert.Empty(t, cache.values, "released even when the run failed")
}

func TestWithLock_CacheErrorIsNotARun(t *testing.T) {
	cache := newMemoryCache()
	cache.setErr = errors.New("redis down")

	ok, err := NewLocker(cache).WithLock(context.Background(), "k", time.Minute, func(context.Context) error {
		t.Fatal("must not run")

		return nil
	})

	assert.False(t, ok)
	assert.ErrorIs(t, err, cache.setErr)
}

func TestLockTTL_TwiceTheIntervalButNeverUnderTwoMinutes(t *testing.T) {
	assert.Equal(t, 2*time.Minute, LockTTL(30*time.Second))
	assert.Equal(t, 30*time.Minute, LockTTL(15*time.Minute))
}
