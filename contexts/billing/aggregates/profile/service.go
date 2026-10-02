package profile

import (
	"context"

	"github.com/google/uuid"
)

//go:generate mockgen -destination=../../mock/services/profile.go -package=mockservices -mock_names=IService=MockIProfileService github.com/piggystats/nodera/contexts/billing/aggregates/profile IService
type IService interface {
	FindByTeamID(ctx context.Context, teamID uuid.UUID) (*Profile, error)
	Upsert(ctx context.Context, p *Profile) error
}

type IRepository interface {
	FindByTeamID(ctx context.Context, teamID uuid.UUID) (*Profile, error)
	Upsert(ctx context.Context, p *Profile) error
}

type Service struct {
	repository IRepository
}

func NewService(repository IRepository) *Service {
	return &Service{repository: repository}
}

func (s *Service) FindByTeamID(ctx context.Context, teamID uuid.UUID) (*Profile, error) {
	return s.repository.FindByTeamID(ctx, teamID)
}

// Upsert validates the tax identifiers before it writes; the handler owns the
// country-change rule because it needs the Payment methods.
func (s *Service) Upsert(ctx context.Context, p *Profile) error {
	if err := p.Validate(); err != nil {
		return err
	}

	return s.repository.Upsert(ctx, p)
}
