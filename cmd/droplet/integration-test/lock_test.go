package integrationtest

import (
	"context"
	"time"

	"github.com/piggystats/nodera/contexts/droplet/application/jobs"
)

const lockKey = "droplet:lock:test"

// A holder whose lease expired must not release the lock a later holder
// took: the release compares its own token before deleting.
func (s *dropletSuite) TestLock_ReleaseKeepsAnotherHoldersLock() {
	ctx := context.Background()
	defer s.deleteByPattern(lockKey)
	locker := jobs.NewLocker(s.redis)

	ran, err := locker.WithLock(ctx, lockKey, time.Minute, func(ctx context.Context) error {
		// The lease is lost mid-run and someone else takes the lock.
		s.Require().NoError(s.redis.Delete(ctx, lockKey))
		s.Require().NoError(s.redis.Set(ctx, lockKey, "someone-else", time.Minute))

		return nil
	})

	s.Require().NoError(err)
	s.True(ran)
	value, err := s.redis.GetString(ctx, lockKey)
	s.Require().NoError(err)
	s.Equal("someone-else", value, "the other holder's lock survived our release")
}

func (s *dropletSuite) TestLock_SecondHolderSkipsAndFirstReleases() {
	ctx := context.Background()
	defer s.deleteByPattern(lockKey)
	locker := jobs.NewLocker(s.redis)

	ran, err := locker.WithLock(ctx, lockKey, time.Minute, func(ctx context.Context) error {
		nested, err := locker.WithLock(ctx, lockKey, time.Minute, func(context.Context) error { return nil })
		s.Require().NoError(err)
		s.False(nested, "a run that finds the lock taken skips its turn")

		return nil
	})
	s.Require().NoError(err)
	s.True(ran)

	exists, err := s.redis.Exists(ctx, lockKey)
	s.Require().NoError(err)
	s.False(exists, "released once the run finished")
}
