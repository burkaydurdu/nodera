package access

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/pkg/authz"
)

//go:generate mockgen -destination=../../mock/services/access.go -package=mockservices -mock_names=IService=MockIAccessService github.com/piggystats/nodera/contexts/droplet/aggregates/access IService
type IService interface {
	UpsertProject(ctx context.Context, projectID, teamID uuid.UUID) error
	UpsertTeamMember(ctx context.Context, teamID, userID uuid.UUID, role authz.Role, addedAt time.Time) error
	UpdateTeamMemberRole(ctx context.Context, teamID, userID uuid.UUID, role authz.Role, changedAt time.Time) error
	RemoveTeamMember(ctx context.Context, teamID, userID uuid.UUID, removedAt time.Time) error
	FindProjectMembership(ctx context.Context, projectID, userID uuid.UUID) (*Membership, error)
	FindTeamMembership(ctx context.Context, teamID, userID uuid.UUID) (*Membership, error)
}

type IRepository interface {
	UpsertProject(ctx context.Context, projectID, teamID uuid.UUID) error
	UpsertTeamMember(ctx context.Context, teamID, userID uuid.UUID, role authz.Role, addedAt time.Time) error
	UpdateTeamMemberRole(ctx context.Context, teamID, userID uuid.UUID, role authz.Role, changedAt time.Time) error
	RemoveTeamMember(ctx context.Context, teamID, userID uuid.UUID, removedAt time.Time) error
	FindProjectMembership(ctx context.Context, projectID, userID uuid.UUID) (*Membership, error)
	FindTeamMembership(ctx context.Context, teamID, userID uuid.UUID) (*Membership, error)
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

func (s *Service) UpsertTeamMember(ctx context.Context, teamID, userID uuid.UUID, role authz.Role, addedAt time.Time) error {
	return s.repository.UpsertTeamMember(ctx, teamID, userID, role, addedAt)
}

func (s *Service) UpdateTeamMemberRole(ctx context.Context, teamID, userID uuid.UUID, role authz.Role, changedAt time.Time) error {
	return s.repository.UpdateTeamMemberRole(ctx, teamID, userID, role, changedAt)
}

func (s *Service) RemoveTeamMember(ctx context.Context, teamID, userID uuid.UUID, removedAt time.Time) error {
	return s.repository.RemoveTeamMember(ctx, teamID, userID, removedAt)
}

func (s *Service) FindProjectMembership(ctx context.Context, projectID, userID uuid.UUID) (*Membership, error) {
	return s.repository.FindProjectMembership(ctx, projectID, userID)
}

func (s *Service) FindTeamMembership(ctx context.Context, teamID, userID uuid.UUID) (*Membership, error) {
	return s.repository.FindTeamMembership(ctx, teamID, userID)
}
