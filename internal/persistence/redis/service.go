package redis

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

//go:generate mockgen -destination=../../../mock/persistence/redis_service.go -package=mockpersistence github.com/piggystats/nodera/internal/persistence/redis IService
type IService interface {
	Set(ctx context.Context, key string, value any, expiration time.Duration) error
	SetNX(ctx context.Context, key string, value any, expiration time.Duration) (bool, error)
	GetString(ctx context.Context, key string) (string, error)
	Exists(ctx context.Context, key string) (bool, error)
	Delete(ctx context.Context, keys ...string) error
	CompareAndDelete(ctx context.Context, key, value string) (bool, error)
	Expire(ctx context.Context, key string, expiration time.Duration) error
	SetAdd(ctx context.Context, key string, expiration time.Duration, members ...string) error
	SetRemove(ctx context.Context, key string, members ...string) error
	SetMembers(ctx context.Context, key string) ([]string, error)
	Update(ctx context.Context, key string, mutate func(current string) (next string, ttl time.Duration, skip bool, err error)) error
	SlidingWindowAllow(ctx context.Context, key string, window time.Duration, limit int64) (bool, error)
}

type Service struct {
	client IClient
}

func NewService(client IClient) IService {
	return &Service{client: client}
}

func (s *Service) Set(ctx context.Context, key string, value any, expiration time.Duration) error {
	return s.client.GetClient().Set(ctx, key, value, expiration).Err()
}

func (s *Service) SetNX(ctx context.Context, key string, value any, expiration time.Duration) (bool, error) {
	return s.client.GetClient().SetNX(ctx, key, value, expiration).Result()
}

// GetString answers "" with no error for an absent key, so callers treat a
// missing code and an expired code the same way.
func (s *Service) GetString(ctx context.Context, key string) (string, error) {
	value, err := s.client.GetClient().Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}

	return value, err
}

func (s *Service) Exists(ctx context.Context, key string) (bool, error) {
	n, err := s.client.GetClient().Exists(ctx, key).Result()

	return n > 0, err
}

func (s *Service) Delete(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}

	return s.client.GetClient().Unlink(ctx, keys...).Err()
}

// compareAndDelete is the lock-release script: the key goes only when it
// still holds the caller's value, in one server-side step, so a holder whose
// lease expired cannot free a lock a later holder took.
var compareAndDelete = redis.NewScript(`
	if redis.call("GET", KEYS[1]) == ARGV[1] then
		return redis.call("DEL", KEYS[1])
	end
	return 0`)

// CompareAndDelete removes key only while it holds value and reports whether
// it did.
func (s *Service) CompareAndDelete(ctx context.Context, key, value string) (bool, error) {
	n, err := compareAndDelete.Run(ctx, s.client.GetClient(), []string{key}, value).Int64()
	if err != nil {
		return false, err
	}

	return n > 0, nil
}

func (s *Service) Expire(ctx context.Context, key string, expiration time.Duration) error {
	return s.client.GetClient().Expire(ctx, key, expiration).Err()
}

func (s *Service) SetAdd(ctx context.Context, key string, expiration time.Duration, members ...string) error {
	args := make([]any, 0, len(members))
	for _, m := range members {
		args = append(args, m)
	}

	pipe := s.client.GetClient().TxPipeline()
	pipe.SAdd(ctx, key, args...)
	pipe.Expire(ctx, key, expiration)
	_, err := pipe.Exec(ctx)

	return err
}

func (s *Service) SetRemove(ctx context.Context, key string, members ...string) error {
	args := make([]any, 0, len(members))
	for _, m := range members {
		args = append(args, m)
	}

	return s.client.GetClient().SRem(ctx, key, args...).Err()
}

func (s *Service) SetMembers(ctx context.Context, key string) ([]string, error) {
	return s.client.GetClient().SMembers(ctx, key).Result()
}

const updateMaxRetries = 8

// Update is an atomic read-modify-write through WATCH/MULTI: mutate sees the
// current value ("" when absent) and answers what to store, or skip. It is the
// primitive behind every attempt counter here, so concurrent guesses cannot
// clobber each other's increments.
func (s *Service) Update(ctx context.Context, key string, mutate func(current string) (string, time.Duration, bool, error)) error {
	client := s.client.GetClient()

	txf := func(tx *redis.Tx) error {
		current, err := tx.Get(ctx, key).Result()
		if errors.Is(err, redis.Nil) {
			current = ""
		} else if err != nil {
			return err
		}

		next, ttl, skip, err := mutate(current)
		if err != nil {
			return err
		}
		if skip {
			return nil
		}

		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.Set(ctx, key, next, ttl)

			return nil
		})

		return err
	}

	for i := 0; i < updateMaxRetries; i++ {
		err := client.Watch(ctx, txf, key)
		if err == nil {
			return nil
		}
		if errors.Is(err, redis.TxFailedErr) {
			continue
		}

		return err
	}

	return redis.TxFailedErr
}

// SlidingWindowAllow records one hit on key and reports whether the window
// still has room. A sorted set of hit timestamps trimmed to the window is the
// whole state; the key expires with the window so idle keys cost nothing.
func (s *Service) SlidingWindowAllow(ctx context.Context, key string, window time.Duration, limit int64) (bool, error) {
	now := time.Now().UnixNano()
	windowStart := now - window.Nanoseconds()

	pipe := s.client.GetClient().TxPipeline()
	pipe.ZRemRangeByScore(ctx, key, "0", strconv.FormatInt(windowStart, 10))
	count := pipe.ZCard(ctx, key)
	pipe.ZAdd(ctx, key, redis.Z{Score: float64(now), Member: strconv.FormatInt(now, 10)})
	pipe.Expire(ctx, key, window)
	if _, err := pipe.Exec(ctx); err != nil {
		return false, err
	}

	return count.Val() < limit, nil
}
