package apikey

import (
	"context"

	"github.com/google/uuid"
)

//go:generate mockgen -destination=../../mock/services/apikey.go -package=mockservices -mock_names=IService=MockIAPIKeyService github.com/piggystats/nodera/contexts/customer/aggregates/apikey IService
type IService interface {
	Create(ctx context.Context, key *APIKey) error
	CountActive(ctx context.Context, projectID uuid.UUID) (int, error)
	FindActiveByProjectID(ctx context.Context, projectID uuid.UUID) (APIKeys, error)
	FindAllActive(ctx context.Context) (APIKeys, error)
	FindAllRevoked(ctx context.Context) (APIKeys, error)
	Revoke(ctx context.Context, teamID, projectID uuid.UUID, id string) error
}

type IRepository interface {
	Create(ctx context.Context, key *APIKey) error
	CountActive(ctx context.Context, projectID uuid.UUID) (int, error)
	FindActiveByProjectID(ctx context.Context, projectID uuid.UUID) (APIKeys, error)
	FindAllActive(ctx context.Context) (APIKeys, error)
	FindAllRevoked(ctx context.Context) (APIKeys, error)
	Revoke(ctx context.Context, teamID, projectID uuid.UUID, id string) error
}

type Service struct {
	repository IRepository
}

func NewService(repository IRepository) *Service {
	return &Service{repository: repository}
}

func (s *Service) Create(ctx context.Context, key *APIKey) error {
	return s.repository.Create(ctx, key)
}

func (s *Service) CountActive(ctx context.Context, projectID uuid.UUID) (int, error) {
	return s.repository.CountActive(ctx, projectID)
}

func (s *Service) FindActiveByProjectID(ctx context.Context, projectID uuid.UUID) (APIKeys, error) {
	return s.repository.FindActiveByProjectID(ctx, projectID)
}

func (s *Service) FindAllActive(ctx context.Context) (APIKeys, error) {
	return s.repository.FindAllActive(ctx)
}

func (s *Service) FindAllRevoked(ctx context.Context) (APIKeys, error) {
	return s.repository.FindAllRevoked(ctx)
}

func (s *Service) Revoke(ctx context.Context, teamID, projectID uuid.UUID, id string) error {
	return s.repository.Revoke(ctx, teamID, projectID, id)
}
