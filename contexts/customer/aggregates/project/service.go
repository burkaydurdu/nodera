package project

import (
	"context"

	"github.com/google/uuid"
)

//go:generate mockgen -destination=../../mock/services/project.go -package=mockservices -mock_names=IService=MockIProjectService github.com/piggystats/nodera/contexts/customer/aggregates/project IService
type IService interface {
	Create(ctx context.Context, project *Project) error
	FindByTeamID(ctx context.Context, teamID uuid.UUID) (Projects, error)
	FindAll(ctx context.Context) (Projects, error)
}

type IRepository interface {
	Create(ctx context.Context, project *Project) error
	FindByTeamID(ctx context.Context, teamID uuid.UUID) (Projects, error)
	FindAll(ctx context.Context) (Projects, error)
}

type Service struct {
	repository IRepository
}

func NewService(repository IRepository) *Service {
	return &Service{repository: repository}
}

func (s *Service) Create(ctx context.Context, project *Project) error {
	return s.repository.Create(ctx, project)
}

func (s *Service) FindByTeamID(ctx context.Context, teamID uuid.UUID) (Projects, error) {
	return s.repository.FindByTeamID(ctx, teamID)
}

func (s *Service) FindAll(ctx context.Context) (Projects, error) {
	return s.repository.FindAll(ctx)
}
