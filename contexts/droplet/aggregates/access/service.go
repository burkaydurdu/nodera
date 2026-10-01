package access

import (
	"context"

	"github.com/google/uuid"
)

//go:generate mockgen -destination=../../mock/services/access.go -package=mockservices -mock_names=IService=MockIAccessService github.com/piggystats/nodera/contexts/droplet/aggregates/access IService
type IService interface {
	UpsertProject(ctx context.Context, projectID, teamID uuid.UUID) error
	UpsertTeamMember(ctx context.Context, teamID, userID uuid.UUID, role string) error
	FindProjectTeamForMember(ctx context.Context, projectID, userID uuid.UUID) (uuid.UUID, error)
	IsTeamMember(ctx context.Context, teamID, userID uuid.UUID) (bool, error)
}

type IRepository interface {
	UpsertProject(ctx context.Context, projectID, teamID uuid.UUID) error
	UpsertTeamMember(ctx context.Context, teamID, userID uuid.UUID, role string) error
	FindProjectTeamForMember(ctx context.Context, projectID, userID uuid.UUID) (uuid.UUID, error)
	IsTeamMember(ctx context.Context, teamID, userID uuid.UUID) (bool, error)
}

type Service struct {
	repository IRepository
}

func NewService(repository IRepository) *Service {
	return &Service{repository: repository}
}

func (s *Service) UpsertProject(ctx context.Context, projectID, teamID uuid.UUID) error {
	return s.repository.UpsertProject(ctx, projectID, teamID)
}

func (s *Service) UpsertTeamMember(ctx context.Context, teamID, userID uuid.UUID, role string) error {
	return s.repository.UpsertTeamMember(ctx, teamID, userID, role)
}

func (s *Service) FindProjectTeamForMember(ctx context.Context, projectID, userID uuid.UUID) (uuid.UUID, error) {
	return s.repository.FindProjectTeamForMember(ctx, projectID, userID)
}

func (s *Service) IsTeamMember(ctx context.Context, teamID, userID uuid.UUID) (bool, error) {
	return s.repository.IsTeamMember(ctx, teamID, userID)
}
