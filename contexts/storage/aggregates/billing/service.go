package billing

import (
	"context"

	"github.com/google/uuid"
)

//go:generate mockgen -destination=../../mock/services/billing.go -package=mockservices -mock_names=IService=MockIBillingService github.com/piggystats/nodera/contexts/storage/aggregates/billing IService
type IService interface {
	Upsert(ctx context.Context, tb *TeamBilling) (bool, error)
	StatusOf(ctx context.Context, teamID uuid.UUID) (Status, error)
}

type IRepository interface {
	Upsert(ctx context.Context, tb *TeamBilling) (bool, error)
	Find(ctx context.Context, teamID uuid.UUID) (*TeamBilling, error)
}

type Service struct {
	repository IRepository
}

func NewService(repository IRepository) *Service {
	return &Service{repository: repository}
}

func (s *Service) Upsert(ctx context.Context, tb *TeamBilling) (bool, error) {
	return s.repository.Upsert(ctx, tb)
}

// StatusOf fails closed: a Team never heard about has no payment method.
func (s *Service) StatusOf(ctx context.Context, teamID uuid.UUID) (Status, error) {
	tb, err := s.repository.Find(ctx, teamID)
	if err != nil {
		return "", err
	}
	if tb == nil {
		return StatusNoPaymentMethod, nil
	}

	return tb.Status, nil
}
