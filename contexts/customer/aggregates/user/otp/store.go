// Package otp keeps short-lived numeric codes with an attempt budget and a
// resend cooldown: the shape shared by email verification and the email MFA
// code. State lives in Redis and every key carries a TTL.
package otp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/piggystats/nodera/utils/hash"
	"github.com/piggystats/nodera/utils/str"
)

const CodeLength = 6

type Cache interface {
	Set(ctx context.Context, key string, value any, expiration time.Duration) error
	SetNX(ctx context.Context, key string, value any, expiration time.Duration) (bool, error)
	GetString(ctx context.Context, key string) (string, error)
	Delete(ctx context.Context, keys ...string) error
	Update(ctx context.Context, key string, mutate func(current string) (next string, ttl time.Duration, skip bool, err error)) error
}

type Policy struct {
	// Prefix names the purpose, e.g. "verify:email"; the subject (user id) is
	// appended.
	Prefix      string
	TTL         time.Duration
	MaxAttempts int
	Cooldown    time.Duration
	// DevCode, when set, replaces the random code (dev environment only).
	DevCode string
}

type record struct {
	Code     string `json:"code"`
	Attempts int    `json:"attempts"`
	// Consumed is set inside the atomic verify, so a second concurrent
	// submission of the same right code finds it spent.
	Consumed bool `json:"consumed"`
}

// consumedTTL keeps the spent marker around briefly; a resend replaces it.
const consumedTTL = time.Minute

type Store struct {
	cache  Cache
	policy Policy
}

func NewStore(cache Cache, policy Policy) *Store {
	return &Store{cache: cache, policy: policy}
}

// Issue mints a code for subject and stores it, replacing any earlier one. It
// refuses while the cooldown from the previous issue is still running.
func (s *Store) Issue(ctx context.Context, subject string) (string, error) {
	claimed, err := s.cache.SetNX(ctx, s.cooldownKey(subject), "1", s.policy.Cooldown)
	if err != nil {
		return "", err
	}
	if !claimed {
		return "", ErrorCooldown.Copy().Add("retry_after_seconds", int(s.policy.Cooldown.Seconds()))
	}

	return s.store(ctx, subject)
}

// Renew is Issue for a flow that starts over (a fresh sign-in): it ignores a
// running cooldown but starts a new one, so the resend budget still holds.
func (s *Store) Renew(ctx context.Context, subject string) (string, error) {
	if err := s.cache.Set(ctx, s.cooldownKey(subject), "1", s.policy.Cooldown); err != nil {
		return "", err
	}

	return s.store(ctx, subject)
}

func (s *Store) store(ctx context.Context, subject string) (code string, err error) {
	code = s.policy.DevCode
	if str.IsEmpty(code) {
		code, err = hash.RandomDigits(CodeLength)
		if err != nil {
			return "", err
		}
	}

	payload, err := json.Marshal(record{Code: code})
	if err != nil {
		return "", err
	}

	if err := s.cache.Set(ctx, s.codeKey(subject), string(payload), s.policy.TTL); err != nil {
		return "", err
	}

	return code, nil
}

// Verify checks code against the stored one. A miss burns one attempt; the
// last miss spends the code, so a guess never outlives the budget. A hit
// spends it too: a code is single-use, and the spend happens inside the
// atomic read-modify-write so concurrent submissions cannot both win.
func (s *Store) Verify(ctx context.Context, subject, code string) error {
	var outcome error

	err := s.cache.Update(ctx, s.codeKey(subject), func(current string) (string, time.Duration, bool, error) {
		if str.IsEmpty(current) {
			outcome = ErrorInvalidCode
			return "", 0, true, nil
		}

		var rec record
		if err := json.Unmarshal([]byte(current), &rec); err != nil || rec.Consumed {
			outcome = ErrorInvalidCode
			return "", 0, true, nil
		}

		if hash.ConstantTimeEquals(rec.Code, code) {
			outcome = nil
			payload, mErr := json.Marshal(record{Consumed: true})

			return string(payload), consumedTTL, false, mErr
		}

		rec.Attempts++
		remaining := s.policy.MaxAttempts - rec.Attempts
		if remaining <= 0 {
			outcome = ErrorAttemptsExhausted
			payload, mErr := json.Marshal(record{Consumed: true})

			return string(payload), consumedTTL, false, mErr
		}

		outcome = ErrorInvalidCode.Copy().Add("attempts_left", remaining)
		payload, mErr := json.Marshal(rec)

		return string(payload), s.policy.TTL, false, mErr
	})
	if err != nil {
		return err
	}

	return outcome
}

// Clear drops the code and the cooldown, for a subject whose flow is over.
func (s *Store) Clear(ctx context.Context, subject string) error {
	return s.cache.Delete(ctx, s.codeKey(subject), s.cooldownKey(subject))
}

func (s *Store) CooldownSeconds() int {
	return int(s.policy.Cooldown.Seconds())
}

func (s *Store) codeKey(subject string) string {
	return fmt.Sprintf("%s:%s", s.policy.Prefix, subject)
}

func (s *Store) cooldownKey(subject string) string {
	return fmt.Sprintf("%s:cooldown:%s", s.policy.Prefix, subject)
}
