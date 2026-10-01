package provider

import (
	"context"

	"github.com/google/uuid"
)

//go:generate mockgen -destination=../../mock/services/provider.go -package=mockservices -mock_names=IService=MockIProviderService github.com/piggystats/nodera/contexts/droplet/aggregates/provider IService
type IService interface {
	FindEnabled(ctx context.Context) (Providers, error)
	FindByID(ctx context.Context, id uuid.UUID) (*Provider, error)
	FindByCode(ctx context.Context, code string) (*Provider, error)
}

type IRepository interface {
	FindEnabled(ctx context.Context) (Providers, error)
	FindByID(ctx context.Context, id uuid.UUID) (*Provider, error)
	FindByCode(ctx context.Context, code string) (*Provider, error)
}

type Service struct {
	repository IRepository
}

func NewService(repository IRepository) *Service {
	return &Service{repository: repository}
}

func (s *Service) FindEnabled(ctx context.Context) (Providers, error) {
	return s.repository.FindEnabled(ctx)
}

func (s *Service) FindByID(ctx context.Context, id uuid.UUID) (*Provider, error) {
	return s.repository.FindByID(ctx, id)
}

func (s *Service) FindByCode(ctx context.Context, code string) (*Provider, error) {
	return s.repository.FindByCode(ctx, code)
}
