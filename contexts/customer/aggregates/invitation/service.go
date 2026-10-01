package invitation

import (
	"context"
	"time"

	"github.com/google/uuid"
)

//go:generate mockgen -destination=../../mock/services/invitation.go -package=mockservices -mock_names=IService=MockIInvitationService github.com/piggystats/nodera/contexts/customer/aggregates/invitation IService
type IService interface {
	Create(ctx context.Context, inv *Invitation) error
	ExpireStale(ctx context.Context, teamID uuid.UUID, email string) error
	FindByID(ctx context.Context, id uuid.UUID) (*Invitation, error)
	FindByTokenHash(ctx context.Context, tokenHash string) (*Invitation, error)
	FindByTeamID(ctx context.Context, teamID uuid.UUID) (Invitations, error)
	FindPendingByEmail(ctx context.Context, email string) (Invitations, error)
	Reissue(ctx context.Context, id uuid.UUID, tokenHash string, expiresAt time.Time) error
	Respond(ctx context.Context, id uuid.UUID, status Status, acceptedBy *uuid.UUID) error
}

type IRepository interface {
	Create(ctx context.Context, inv *Invitation) error
	ExpireStale(ctx context.Context, teamID uuid.UUID, email string) error
	FindByID(ctx context.Context, id uuid.UUID) (*Invitation, error)
	FindByTokenHash(ctx context.Context, tokenHash string) (*Invitation, error)
	FindByTeamID(ctx context.Context, teamID uuid.UUID) (Invitations, error)
	FindPendingByEmail(ctx context.Context, email string) (Invitations, error)
	Reissue(ctx context.Context, id uuid.UUID, tokenHash string, expiresAt time.Time) error
	Respond(ctx context.Context, id uuid.UUID, status Status, acceptedBy *uuid.UUID) error
}

type Service struct {
	repository IRepository
}

func NewService(repository IRepository) *Service {
	return &Service{repository: repository}
}

func (s *Service) Create(ctx context.Context, inv *Invitation) error {
	return s.repository.Create(ctx, inv)
}

func (s *Service) ExpireStale(ctx context.Context, teamID uuid.UUID, email string) error {
	return s.repository.ExpireStale(ctx, teamID, email)
}

func (s *Service) FindByID(ctx context.Context, id uuid.UUID) (*Invitation, error) {
	return s.repository.FindByID(ctx, id)
}

func (s *Service) FindByTokenHash(ctx context.Context, tokenHash string) (*Invitation, error) {
	return s.repository.FindByTokenHash(ctx, tokenHash)
}

func (s *Service) FindByTeamID(ctx context.Context, teamID uuid.UUID) (Invitations, error) {
	return s.repository.FindByTeamID(ctx, teamID)
}

func (s *Service) FindPendingByEmail(ctx context.Context, email string) (Invitations, error) {
	return s.repository.FindPendingByEmail(ctx, email)
}

func (s *Service) Reissue(ctx context.Context, id uuid.UUID, tokenHash string, expiresAt time.Time) error {
	return s.repository.Reissue(ctx, id, tokenHash, expiresAt)
}

func (s *Service) Respond(ctx context.Context, id uuid.UUID, status Status, acceptedBy *uuid.UUID) error {
	return s.repository.Respond(ctx, id, status, acceptedBy)
}
