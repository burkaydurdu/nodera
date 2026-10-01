package sshkey

import (
	"context"

	"github.com/google/uuid"
)

//go:generate mockgen -destination=../../mock/services/sshkey.go -package=mockservices -mock_names=IService=MockISSHKeyService github.com/piggystats/nodera/contexts/customer/aggregates/sshkey IService
type IService interface {
	Create(ctx context.Context, key *SSHKey) error
	FindByTeamID(ctx context.Context, teamID uuid.UUID) (SSHKeys, error)
	Delete(ctx context.Context, teamID, keyID uuid.UUID) error
}

type IRepository interface {
	Create(ctx context.Context, key *SSHKey) error
	FindByTeamID(ctx context.Context, teamID uuid.UUID) (SSHKeys, error)
	Delete(ctx context.Context, teamID, keyID uuid.UUID) error
}

type Service struct {
	repository IRepository
}

func NewService(repository IRepository) *Service {
	return &Service{repository: repository}
}

func (s *Service) Create(ctx context.Context, key *SSHKey) error {
	return s.repository.Create(ctx, key)
}

func (s *Service) FindByTeamID(ctx context.Context, teamID uuid.UUID) (SSHKeys, error) {
	return s.repository.FindByTeamID(ctx, teamID)
}

func (s *Service) Delete(ctx context.Context, teamID, keyID uuid.UUID) error {
	return s.repository.Delete(ctx, teamID, keyID)
}
