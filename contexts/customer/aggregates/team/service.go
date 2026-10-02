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
	FindAll(ctx context.Context) (TeamDetails, error)
	FindAllMembers(ctx context.Context) (MemberDetails, error)
	FindMembers(ctx context.Context, teamID uuid.UUID) (MemberDetails, error)
	FindMember(ctx context.Context, teamID, userID uuid.UUID) (*MemberDetail, error)
	Lock(ctx context.Context, teamID uuid.UUID) error
	CountOwners(ctx context.Context, teamID uuid.UUID) (int, error)
	UpdateMemberRole(ctx context.Context, teamID, userID uuid.UUID, role Role) error
	RemoveMember(ctx context.Context, teamID, userID uuid.UUID) error
}

type IRepository interface {
	Create(ctx context.Context, team *Team) error
	AddMember(ctx context.Context, teamID, userID uuid.UUID, role Role) (*Member, error)
	FindMembershipsByUserID(ctx context.Context, userID uuid.UUID) (Memberships, error)
	FindMembership(ctx context.Context, teamID, userID uuid.UUID) (*Membership, error)
	FindAll(ctx context.Context) (TeamDetails, error)
	FindAllMembers(ctx context.Context) (MemberDetails, error)
	FindMembers(ctx context.Context, teamID uuid.UUID) (MemberDetails, error)
	FindMember(ctx context.Context, teamID, userID uuid.UUID) (*MemberDetail, error)
	Lock(ctx context.Context, teamID uuid.UUID) error
	CountOwners(ctx context.Context, teamID uuid.UUID) (int, error)
	UpdateMemberRole(ctx context.Context, teamID, userID uuid.UUID, role Role) error
	RemoveMember(ctx context.Context, teamID, userID uuid.UUID) error
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

func (s *Service) FindAll(ctx context.Context) (TeamDetails, error) {
	return s.repository.FindAll(ctx)
}

func (s *Service) FindAllMembers(ctx context.Context) (MemberDetails, error) {
	return s.repository.FindAllMembers(ctx)
}

func (s *Service) FindMembers(ctx context.Context, teamID uuid.UUID) (MemberDetails, error) {
	return s.repository.FindMembers(ctx, teamID)
}

func (s *Service) FindMember(ctx context.Context, teamID, userID uuid.UUID) (*MemberDetail, error) {
	return s.repository.FindMember(ctx, teamID, userID)
}

func (s *Service) Lock(ctx context.Context, teamID uuid.UUID) error {
	return s.repository.Lock(ctx, teamID)
}

func (s *Service) CountOwners(ctx context.Context, teamID uuid.UUID) (int, error) {
	return s.repository.CountOwners(ctx, teamID)
}

func (s *Service) UpdateMemberRole(ctx context.Context, teamID, userID uuid.UUID, role Role) error {
	return s.repository.UpdateMemberRole(ctx, teamID, userID, role)
}

func (s *Service) RemoveMember(ctx context.Context, teamID, userID uuid.UUID) error {
	return s.repository.RemoveMember(ctx, teamID, userID)
}
