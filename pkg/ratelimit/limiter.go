// Package ratelimit is the fine-grained request budget on the public
// authentication endpoints, per source address and per email.
package ratelimit

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/piggystats/nodera/pkg/apperror"
	"github.com/piggystats/nodera/utils/hash"
)

type Cache interface {
	SlidingWindowAllow(ctx context.Context, key string, window time.Duration, limit int64) (bool, error)
}

type Rule struct {
	Window time.Duration
	Limit  int64
}

//go:generate mockgen -destination=../../mock/ratelimit/limiter.go -package=mockratelimit github.com/piggystats/nodera/pkg/ratelimit ILimiter
type ILimiter interface {
	// Allow records a hit for subject under scope and returns
	// apperror.ErrorRateLimited when the budget is spent. A cache failure
	// lets the request through: the limiter degrades open.
	Allow(ctx context.Context, scope, subject string, rule Rule) error
}

type Limiter struct {
	cache Cache
}

func NewLimiter(cache Cache) *Limiter {
	return &Limiter{cache: cache}
}

func (l *Limiter) Allow(ctx context.Context, scope, subject string, rule Rule) error {
	key := fmt.Sprintf("ratelimit:%s:%s", scope, hash.SHA256Hex(strings.ToLower(strings.TrimSpace(subject))))

	allowed, err := l.cache.SlidingWindowAllow(ctx, key, rule.Window, rule.Limit)
	if err != nil {
		return nil
	}
	if !allowed {
		return apperror.ErrorRateLimited.Copy().Add("retry_after_seconds", int(rule.Window.Seconds()))
	}

	return nil
}
