package threshold

import (
	"context"
	"time"
)

//go:generate mockgen -destination=../../mock/services/threshold.go -package=mockservices -mock_names=IService=MockIThresholdService github.com/piggystats/nodera/contexts/billing/aggregates/threshold IService
type IService interface {
	// For answers the Threshold of a Team that began at origin.
	For(ctx context.Context, origin, now time.Time) (*Threshold, error)
}

type IRepository interface {
	Find(ctx context.Context, kind Kind) (*Threshold, error)
}

type Service struct {
	repository IRepository
}

func NewService(repository IRepository) *Service {
	return &Service{repository: repository}
}

func (s *Service) For(ctx context.Context, origin, now time.Time) (*Threshold, error) {
	return s.repository.Find(ctx, KindFor(origin, now))
}
