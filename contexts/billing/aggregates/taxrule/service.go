package taxrule

import (
	"context"
	"time"
)

//go:generate mockgen -destination=../../mock/services/taxrule.go -package=mockservices -mock_names=IService=MockITaxRuleService github.com/piggystats/nodera/contexts/billing/aggregates/taxrule IService
type IService interface {
	Lookup(ctx context.Context, country, accountType string, at time.Time) (*TaxRule, error)
}

type IRepository interface {
	Lookup(ctx context.Context, country, accountType string, at time.Time) (*TaxRule, error)
}

type Service struct {
	repository IRepository
}

func NewService(repository IRepository) *Service {
	return &Service{repository: repository}
}

func (s *Service) Lookup(ctx context.Context, country, accountType string, at time.Time) (*TaxRule, error) {
	return s.repository.Lookup(ctx, country, accountType, at)
}
