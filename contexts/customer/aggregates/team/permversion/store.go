// Package permversion keeps each User's permission version: the integer the
// access token carries as `pv` and the gateway compares with Redis on every
// request (docs/adr/0005). Any change to one of the User's memberships bumps
// it, so a token minted before the change stops working at once.
package permversion

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/utils/str"
)

const KeyFormat = "user_perm_version:%s"

type Cache interface {
	GetString(ctx context.Context, key string) (string, error)
	Update(ctx context.Context, key string, mutate func(current string) (next string, ttl time.Duration, skip bool, err error)) error
}

type Store struct {
	cache Cache
}

func NewStore(cache Cache) *Store {
	return &Store{cache: cache}
}

// Bump increments the version. It never expires: a missing key reads as 0,
// and a bump after an eviction would still move the value away from any
// token's claim.
func (s *Store) Bump(ctx context.Context, userID uuid.UUID) error {
	return s.cache.Update(ctx, key(userID), func(current string) (string, time.Duration, bool, error) {
		return strconv.FormatInt(parse(current)+1, 10), 0, false, nil
	})
}

// Current answers the version to stamp into a token; a missing key is 0.
func (s *Store) Current(ctx context.Context, userID uuid.UUID) (int64, error) {
	raw, err := s.cache.GetString(ctx, key(userID))
	if err != nil {
		return 0, err
	}

	return parse(raw), nil
}

func key(userID uuid.UUID) string {
	return fmt.Sprintf(KeyFormat, userID)
}

func parse(raw string) int64 {
	if str.IsEmpty(raw) {
		return 0
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0
	}

	return n
}
