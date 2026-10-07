package accesskey

import (
	"context"

	"github.com/google/uuid"
)

//go:generate mockgen -destination=../../mock/services/accesskey.go -package=mockservices -mock_names=IService=MockIAccessKeyService github.com/piggystats/nodera/contexts/storage/aggregates/accesskey IService
type IService interface {
	Insert(ctx context.Context, k *AccessKey) error
	Find(ctx context.Context, storageID, id uuid.UUID) (*AccessKey, error)
	ListByStorage(ctx context.Context, storageID uuid.UUID) ([]*AccessKey, error)
	CountByStorage(ctx context.Context, storageID uuid.UUID) (int, error)
	Delete(ctx context.Context, id uuid.UUID) error
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

func (s *Service) Insert(ctx context.Context, k *AccessKey) error {
	return s.repository.Insert(ctx, k)
}

func (s *Service) Find(ctx context.Context, storageID, id uuid.UUID) (*AccessKey, error) {
	return s.repository.Find(ctx, storageID, id)
}

func (s *Service) ListByStorage(ctx context.Context, storageID uuid.UUID) ([]*AccessKey, error) {
	return s.repository.ListByStorage(ctx, storageID)
}

func (s *Service) CountByStorage(ctx context.Context, storageID uuid.UUID) (int, error) {
	return s.repository.CountByStorage(ctx, storageID)
}

func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	return s.repository.Delete(ctx, id)
}

func (s *Service) DeleteByStorage(ctx context.Context, storageID uuid.UUID) error {
	return s.repository.DeleteByStorage(ctx, storageID)
}
