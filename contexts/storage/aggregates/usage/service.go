package usage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

//go:generate mockgen -destination=../../mock/services/usage.go -package=mockservices -mock_names=IService=MockIUsageService github.com/piggystats/nodera/contexts/storage/aggregates/usage IService
type IService interface {
	UpsertSize(ctx context.Context, s *Size) error
	UpsertTraffic(ctx context.Context, hours []*Traffic) error
	ListDays(ctx context.Context, storageID uuid.UUID, from, to time.Time) ([]*Day, error)
	DeleteByStorage(ctx context.Context, storageID uuid.UUID) error
}

type IRepository interface {
	IService
}

type Service struct {
	repository IRepository
}

func NewService(repository IRepository) *Service {
	return &Service{repository: repository}
}

func (s *Service) UpsertSize(ctx context.Context, size *Size) error {
	return s.repository.UpsertSize(ctx, size)
}

func (s *Service) UpsertTraffic(ctx context.Context, hours []*Traffic) error {
	return s.repository.UpsertTraffic(ctx, hours)
}

func (s *Service) ListDays(ctx context.Context, storageID uuid.UUID, from, to time.Time) ([]*Day, error) {
	return s.repository.ListDays(ctx, storageID, from, to)
}

func (s *Service) DeleteByStorage(ctx context.Context, storageID uuid.UUID) error {
	return s.repository.DeleteByStorage(ctx, storageID)
}
