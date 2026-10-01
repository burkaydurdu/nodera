package integrationtest

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
)

// The suite keeps its own handle on the raw client for cleanup; production
// code only ever sees the narrow IService.
var rawRedis *redis.Client

func (s *customerSuite) deleteByPattern(pattern string) {
	if rawRedis == nil {
		return
	}
	ctx := context.Background()
	iter := rawRedis.Scan(ctx, 0, pattern, 100).Iterator()
	var keys []string
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if len(keys) > 0 {
		_ = rawRedis.Del(ctx, keys...).Err()
	}
}

// ageRotations moves every session's last rotation outside the grace window,
// so a reused refresh token is judged as theft instead of a parallel tab.
func (s *customerSuite) ageRotations() {
	ctx := context.Background()
	iter := rawRedis.Scan(ctx, 0, "session:*", 100).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()
		raw, err := rawRedis.Get(ctx, key).Result()
		s.Require().NoError(err)
		var doc map[string]any
		s.Require().NoError(json.Unmarshal([]byte(raw), &doc))
		doc["rotated_at"] = time.Now().Add(-time.Hour).Format(time.RFC3339Nano)
		aged, err := json.Marshal(doc)
		s.Require().NoError(err)
		ttl, err := rawRedis.TTL(ctx, key).Result()
		s.Require().NoError(err)
		s.Require().NoError(rawRedis.Set(ctx, key, aged, ttl).Err())
	}
}
