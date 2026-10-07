// Package joblock serializes a periodic job across a service's worker
// replicas with a Redis lease. It lives outside the contexts because a context
// never imports another.
package joblock

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"
)

// minLockTTL keeps a lease from expiring under a slow run of a job on a short
// interval; LockTTL is the one place the lease length is decided.
const (
	minLockTTL   = 2 * time.Minute
	tokenBytes   = 16
	ttlPerTicker = 2
)

type LockCache interface {
	SetNX(ctx context.Context, key string, value any, expiration time.Duration) (bool, error)
	CompareAndDelete(ctx context.Context, key, value string) (bool, error)
}

// Locker serializes a job across worker replicas: a run that finds the lock
// taken skips its turn. Every acquisition stores its own random token and
// releases only a lock that still carries it, so a holder whose lease ran out
// cannot free the lock a later replica took. The TTL bounds a crashed holder.
type Locker struct {
	cache LockCache
}

func NewLocker(cache LockCache) *Locker {
	return &Locker{cache: cache}
}

// LockTTL is the lease for a job that runs every interval: long enough to
// outlive one late tick, never shorter than minLockTTL.
func LockTTL(interval time.Duration) time.Duration {
	return max(ttlPerTicker*interval, minLockTTL)
}

// WithLock runs fn when the lock is free and answers false when it was not.
func (l *Locker) WithLock(ctx context.Context, key string, ttl time.Duration, fn func(context.Context) error) (bool, error) {
	token, err := newToken()
	if err != nil {
		return false, err
	}

	acquired, err := l.cache.SetNX(ctx, key, token, ttl)
	if err != nil || !acquired {
		return false, err
	}
	defer func() { _, _ = l.cache.CompareAndDelete(ctx, key, token) }()

	return true, fn(ctx)
}

func newToken() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	return hex.EncodeToString(b), nil
}
