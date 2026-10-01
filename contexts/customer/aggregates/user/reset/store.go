// Package reset keeps the single-use password reset tokens.
package reset

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/utils/hash"
	"github.com/piggystats/nodera/utils/str"
)

const (
	TokenTTL    = 1 * time.Hour
	CachePrefix = "password_reset:%s"
	tokenBytes  = 32
)

type Cache interface {
	Set(ctx context.Context, key string, value any, expiration time.Duration) error
	GetString(ctx context.Context, key string) (string, error)
	Delete(ctx context.Context, keys ...string) error
}

type Store struct {
	cache Cache
}

func NewStore(cache Cache) *Store {
	return &Store{cache: cache}
}

func (s *Store) Create(ctx context.Context, userID uuid.UUID) (string, error) {
	token, err := hash.RandomHex(tokenBytes)
	if err != nil {
		return "", err
	}

	if err := s.cache.Set(ctx, fmt.Sprintf(CachePrefix, token), userID.String(), TokenTTL); err != nil {
		return "", err
	}

	return token, nil
}

// Consume answers the User the token was issued for and deletes it, so a
// token works exactly once.
func (s *Store) Consume(ctx context.Context, token string) (uuid.UUID, bool) {
	key := fmt.Sprintf(CachePrefix, token)
	raw, err := s.cache.GetString(ctx, key)
	if err != nil || str.IsEmpty(raw) {
		return uuid.Nil, false
	}

	userID, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, false
	}

	_ = s.cache.Delete(ctx, key)

	return userID, true
}
