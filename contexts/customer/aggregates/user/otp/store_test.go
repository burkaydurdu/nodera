package otp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeCache struct {
	values map[string]string
}

func newFakeCache() *fakeCache { return &fakeCache{values: map[string]string{}} }

func (f *fakeCache) Set(_ context.Context, key string, value any, _ time.Duration) error {
	f.values[key] = value.(string)

	return nil
}

func (f *fakeCache) SetNX(_ context.Context, key string, value any, _ time.Duration) (bool, error) {
	if _, ok := f.values[key]; ok {
		return false, nil
	}
	f.values[key] = value.(string)

	return true, nil
}

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

func newStore(cache Cache) *Store {
	return NewStore(cache, Policy{Prefix: "test", TTL: time.Minute, MaxAttempts: 3, Cooldown: time.Minute})
}

func TestStore_IssueThenVerify_ConsumesTheCode(t *testing.T) {
	store := newStore(newFakeCache())
	ctx := context.Background()

	code, err := store.Issue(ctx, "u1")
	require.NoError(t, err)
	assert.Len(t, code, CodeLength)

	require.NoError(t, store.Verify(ctx, "u1", code))
	assert.True(t, errors.Is(store.Verify(ctx, "u1", code), ErrorInvalidCode), "a code is single-use")
}

func TestStore_SecondIssueWithinCooldown_IsRefused(t *testing.T) {
	store := newStore(newFakeCache())
	ctx := context.Background()
	_, err := store.Issue(ctx, "u1")
	require.NoError(t, err)

	_, err = store.Issue(ctx, "u1")

	assert.True(t, errors.Is(err, ErrorCooldown))
}

func TestStore_RenewIgnoresCooldownAndReplacesTheCode(t *testing.T) {
	cache := newFakeCache()
	store := NewStore(cache, Policy{Prefix: "test", TTL: time.Minute, MaxAttempts: 3, Cooldown: time.Minute, DevCode: ""})
	ctx := context.Background()
	first, _ := store.Issue(ctx, "u1")

	second, err := store.Renew(ctx, "u1")

	require.NoError(t, err)
	assert.True(t, errors.Is(store.Verify(ctx, "u1", first), ErrorInvalidCode) || first == second)
}

func TestStore_ThirdWrongGuess_ExhaustsAndDeletesTheCode(t *testing.T) {
	store := newStore(newFakeCache())
	ctx := context.Background()
	code, _ := store.Issue(ctx, "u1")

	wrong := "000000"
	if wrong == code {
		wrong = "111111"
	}
	err1 := store.Verify(ctx, "u1", wrong)
	err2 := store.Verify(ctx, "u1", wrong)
	err3 := store.Verify(ctx, "u1", wrong)

	assert.True(t, errors.Is(err1, ErrorInvalidCode))
	assert.Equal(t, 2, fieldInt(err1, "attempts_left"))
	assert.Equal(t, 1, fieldInt(err2, "attempts_left"))
	assert.True(t, errors.Is(err3, ErrorAttemptsExhausted))
	assert.True(t, errors.Is(store.Verify(ctx, "u1", code), ErrorInvalidCode), "the right code is dead too")
}

func TestStore_DevCodeReplacesTheRandomOne(t *testing.T) {
	store := NewStore(newFakeCache(), Policy{Prefix: "test", TTL: time.Minute, MaxAttempts: 3, Cooldown: time.Minute, DevCode: "123456"})

	code, err := store.Issue(context.Background(), "u1")

	require.NoError(t, err)
	assert.Equal(t, "123456", code)
}

func fieldInt(err error, key string) int {
	type fielded interface{ GetFields() map[string]any }
	f, ok := err.(fielded)
	if !ok {
		return -1
	}
	v, _ := f.GetFields()[key].(int)

	return v
}
