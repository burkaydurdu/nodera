package mfa

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/utils/hash"
	"github.com/piggystats/nodera/utils/str"
)

const (
	// ChallengeTTL is how long a User has to finish the second step before
	// they must sign in again.
	ChallengeTTL         = 10 * time.Minute
	ChallengeCachePrefix = "mfa:challenge:%s"
	challengeTokenBytes  = 32
)

// Challenge is the pending sign-in between a correct password and a correct
// code, keyed by an opaque token the client echoes back.
type Challenge struct {
	UserID uuid.UUID `json:"user_id"`
	Method Method    `json:"method"`
}

type ChallengeCache interface {
	Set(ctx context.Context, key string, value any, expiration time.Duration) error
	GetString(ctx context.Context, key string) (string, error)
	Delete(ctx context.Context, keys ...string) error
}

type ChallengeStore struct {
	cache ChallengeCache
}

func NewChallengeStore(cache ChallengeCache) *ChallengeStore {
	return &ChallengeStore{cache: cache}
}

func (s *ChallengeStore) Create(ctx context.Context, ch Challenge) (string, error) {
	token, err := hash.RandomHex(challengeTokenBytes)
	if err != nil {
		return "", err
	}

	payload, err := json.Marshal(ch)
	if err != nil {
		return "", err
	}

	if err := s.cache.Set(ctx, fmt.Sprintf(ChallengeCachePrefix, token), string(payload), ChallengeTTL); err != nil {
		return "", err
	}

	return token, nil
}

func (s *ChallengeStore) Get(ctx context.Context, token string) (*Challenge, error) {
	raw, err := s.cache.GetString(ctx, fmt.Sprintf(ChallengeCachePrefix, token))
	if err != nil || str.IsEmpty(raw) {
		return nil, ErrorInvalidChallenge
	}

	var ch Challenge
	if err := json.Unmarshal([]byte(raw), &ch); err != nil {
		return nil, ErrorInvalidChallenge
	}

	return &ch, nil
}

func (s *ChallengeStore) Delete(ctx context.Context, token string) error {
	return s.cache.Delete(ctx, fmt.Sprintf(ChallengeCachePrefix, token))
}
