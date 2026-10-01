package team

import (
	"context"

	"github.com/google/uuid"
)

//go:generate mockgen -destination=../../mock/services/team.go -package=mockservices -mock_names=IService=MockITeamService github.com/piggystats/nodera/contexts/customer/aggregates/team IService
type IService interface {
	Create(ctx context.Context, team *Team) error
	AddMember(ctx context.Context, teamID, userID uuid.UUID, role Role) (*Member, error)
	FindMembershipsByUserID(ctx context.Context, userID uuid.UUID) (Memberships, error)
	FindMembership(ctx context.Context, teamID, userID uuid.UUID) (*Membership, error)
	FindAll(ctx context.Context) (Teams, error)
	FindAllMembers(ctx context.Context) (Members, error)
}

type IRepository interface {
	Create(ctx context.Context, team *Team) error
	AddMember(ctx context.Context, teamID, userID uuid.UUID, role Role) (*Member, error)
	FindMembershipsByUserID(ctx context.Context, userID uuid.UUID) (Memberships, error)
	FindMembership(ctx context.Context, teamID, userID uuid.UUID) (*Membership, error)
	FindAll(ctx context.Context) (Teams, error)
	FindAllMembers(ctx context.Context) (Members, error)
}

type Service struct {
	repository IRepository
}

func NewService(repository IRepository) *Service {
	return &Service{repository: repository}
}

func (s *Service) Create(ctx context.Context, team *Team) error {
	return s.repository.Create(ctx, team)
}

func (s *Service) AddMember(ctx context.Context, teamID, userID uuid.UUID, role Role) (*Member, error) {
	return s.repository.AddMember(ctx, teamID, userID, role)
}

func (s *Service) FindMembershipsByUserID(ctx context.Context, userID uuid.UUID) (Memberships, error) {
	return s.repository.FindMembershipsByUserID(ctx, userID)
}

func (s *Service) FindMembership(ctx context.Context, teamID, userID uuid.UUID) (*Membership, error) {
	return s.repository.FindMembership(ctx, teamID, userID)
}

func (s *Service) FindAll(ctx context.Context) (Teams, error) {
	return s.repository.FindAll(ctx)
}

func (s *Service) FindAllMembers(ctx context.Context) (Members, error) {
	return s.repository.FindAllMembers(ctx)
}
