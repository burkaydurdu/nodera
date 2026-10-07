package middleware

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// SessionKeyFormat is the Redis key the customer context stores a session
// under; the gateway checks the same key. It is a wire contract between the
// contexts, declared here rather than imported.
const SessionKeyFormat = "session:%s"

type Cache interface {
	Exists(ctx context.Context, key string) (bool, error)
}

// RedisSessions answers whether a session record still exists.
type RedisSessions struct {
	cache Cache
}

func NewRedisSessions(cache Cache) *RedisSessions {
	return &RedisSessions{cache: cache}
}

func (s *RedisSessions) Exists(ctx context.Context, sessionID uuid.UUID) (bool, error) {
	return s.cache.Exists(ctx, fmt.Sprintf(SessionKeyFormat, sessionID))
}
