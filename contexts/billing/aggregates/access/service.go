package access

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/pkg/authz"
)

//go:generate mockgen -destination=../../mock/services/access.go -package=mockservices -mock_names=IService=MockIAccessService github.com/piggystats/nodera/contexts/billing/aggregates/access IService
type IService interface {
	UpsertTeam(ctx context.Context, teamID uuid.UUID, name string, createdAt time.Time) error
	UpsertTeamMember(ctx context.Context, m *Member) error
	UpdateTeamMemberRole(ctx context.Context, teamID, userID uuid.UUID, role authz.Role, changedAt time.Time) error
	RemoveTeamMember(ctx context.Context, teamID, userID uuid.UUID, removedAt time.Time) error
	FindTeamMembership(ctx context.Context, teamID, userID uuid.UUID) (*Membership, error)
	FindTeam(ctx context.Context, teamID uuid.UUID) (*Team, error)
}

type IRepository interface {
	UpsertTeam(ctx context.Context, teamID uuid.UUID, name string, createdAt time.Time) error
	UpsertTeamMember(ctx context.Context, m *Member) error
	UpdateTeamMemberRole(ctx context.Context, teamID, userID uuid.UUID, role authz.Role, changedAt time.Time) error
	RemoveTeamMember(ctx context.Context, teamID, userID uuid.UUID, removedAt time.Time) error
	FindTeamMembership(ctx context.Context, teamID, userID uuid.UUID) (*Membership, error)
	FindTeam(ctx context.Context, teamID uuid.UUID) (*Team, error)
}

type Service struct {
	repository IRepository
}

func NewService(repository IRepository) *Service {
	return &Service{repository: repository}
}

func (s *Service) UpsertTeam(ctx context.Context, teamID uuid.UUID, name string, createdAt time.Time) error {
	return s.repository.UpsertTeam(ctx, teamID, name, createdAt)
}

func (s *Service) UpsertTeamMember(ctx context.Context, m *Member) error {
	return s.repository.UpsertTeamMember(ctx, m)
}

func (s *Service) UpdateTeamMemberRole(ctx context.Context, teamID, userID uuid.UUID, role authz.Role, changedAt time.Time) error {
	return s.repository.UpdateTeamMemberRole(ctx, teamID, userID, role, changedAt)
}

func (s *Service) RemoveTeamMember(ctx context.Context, teamID, userID uuid.UUID, removedAt time.Time) error {
	return s.repository.RemoveTeamMember(ctx, teamID, userID, removedAt)
}

func (s *Service) FindTeamMembership(ctx context.Context, teamID, userID uuid.UUID) (*Membership, error) {
	return s.repository.FindTeamMembership(ctx, teamID, userID)
}

func (s *Service) FindTeam(ctx context.Context, teamID uuid.UUID) (*Team, error) {
	return s.repository.FindTeam(ctx, teamID)
}
