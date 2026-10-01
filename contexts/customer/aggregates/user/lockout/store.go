// Package lockout implements the progressive, temporary lock on sign-in: after
// too many failed passwords an email is locked for an escalating period. State
// lives in Redis keyed by a hash of the email, so unknown addresses are tracked
// exactly like real ones and sign-in stays enumeration-safe.
package lockout

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/piggystats/nodera/utils/hash"
	"github.com/piggystats/nodera/utils/str"
)

const (
	// ResetWindow is how long the escalation level survives after the last
	// activity; a quiet hour starts the account fresh at the first tier.
	ResetWindow = 1 * time.Hour
	CachePrefix = "login_lockout:%s"
)

// LockSchedule maps a lock level to its duration; past the end the last entry
// repeats.
var LockSchedule = []time.Duration{
	1 * time.Minute,
	5 * time.Minute,
	15 * time.Minute,
	30 * time.Minute,
	60 * time.Minute,
}

// AttemptSchedule maps a lock level to how many misses trip it: the threshold
// tightens as abuse continues.
var AttemptSchedule = []int{5, 3, 1}

func attemptsForLevel(level int) int {
	if level >= len(AttemptSchedule) {
		return AttemptSchedule[len(AttemptSchedule)-1]
	}

	return AttemptSchedule[level]
}

type Cache interface {
	GetString(ctx context.Context, key string) (string, error)
	Delete(ctx context.Context, keys ...string) error
	Update(ctx context.Context, key string, mutate func(current string) (next string, ttl time.Duration, skip bool, err error)) error
}

type record struct {
	FailCount   int        `json:"fail_count"`
	LockLevel   int        `json:"lock_level"`
	LockedUntil *time.Time `json:"locked_until,omitempty"`
}

type Store struct {
	cache Cache
}

func NewStore(cache Cache) *Store {
	return &Store{cache: cache}
}

// Status reports whether the email is locked and for how much longer. A Redis
// read failure degrades to "not locked": availability over strict enforcement
// on the read path; writes still propagate their errors.
func (s *Store) Status(ctx context.Context, email string) (locked bool, retryAfter time.Duration) {
	raw, err := s.cache.GetString(ctx, key(email))
	if err != nil || str.IsEmpty(raw) {
		return false, 0
	}

	return lockRemaining(decode(raw))
}

// RegisterFailure records one miss. When the count reaches the level's
// threshold it locks the email, bumps the level and resets the count;
// justLocked is true only on the attempt that tripped the lock.
func (s *Store) RegisterFailure(ctx context.Context, email string) (justLocked bool, retryAfter time.Duration, err error) {
	updErr := s.cache.Update(ctx, key(email), func(current string) (string, time.Duration, bool, error) {
		rec := decode(current)

		if locked, remaining := lockRemaining(rec); locked {
			justLocked, retryAfter = false, remaining

			return "", 0, true, nil
		}

		rec.FailCount++
		if rec.FailCount < attemptsForLevel(rec.LockLevel) {
			justLocked, retryAfter = false, 0
			payload, mErr := json.Marshal(rec)

			return string(payload), ResetWindow, false, mErr
		}

		duration := LockSchedule[min(rec.LockLevel, len(LockSchedule)-1)]
		until := time.Now().Add(duration)
		rec.LockedUntil = &until
		rec.LockLevel++
		rec.FailCount = 0

		justLocked, retryAfter = true, duration
		payload, mErr := json.Marshal(rec)

		return string(payload), duration + ResetWindow, false, mErr
	})
	if updErr != nil {
		return false, 0, updErr
	}

	return justLocked, retryAfter, nil
}

// Reset clears the state after a successful sign-in.
func (s *Store) Reset(ctx context.Context, email string) error {
	return s.cache.Delete(ctx, key(email))
}

func decode(raw string) *record {
	var rec record
	if str.IsEmpty(raw) {
		return &rec
	}
	if err := json.Unmarshal([]byte(raw), &rec); err != nil {
		return &record{}
	}

	return &rec
}

func lockRemaining(rec *record) (bool, time.Duration) {
	if rec.LockedUntil == nil {
		return false, 0
	}

	remaining := time.Until(*rec.LockedUntil)
	if remaining <= 0 {
		return false, 0
	}

	return true, remaining
}

func key(email string) string {
	return fmt.Sprintf(CachePrefix, hash.SHA256Hex(strings.ToLower(strings.TrimSpace(email))))
}
