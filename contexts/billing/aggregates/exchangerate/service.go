package exchangerate

import (
	"context"
	"time"
)

//go:generate mockgen -destination=../../mock/services/exchangerate.go -package=mockservices -mock_names=IService=MockIExchangeRateService github.com/piggystats/nodera/contexts/billing/aggregates/exchangerate IService
type IService interface {
	Store(ctx context.Context, rate *Rate) error
	// RateFor answers the day's rate or, when the day has none yet, the latest
	// earlier one; Rate.IsFallbackFor tells which, so the caller can log it.
	RateFor(ctx context.Context, date time.Time) (*Rate, error)
	// RateBefore answers the latest bulletin dated strictly before the day:
	// the central bank publishes in the afternoon the rates that apply from
	// the next day, so an Invoice issued on a day uses the bulletin published
	// before it, whatever the hour of issue. To be confirmed with the
	// accountant.
	RateBefore(ctx context.Context, date time.Time) (*Rate, error)
	Exists(ctx context.Context, date time.Time) (bool, error)
}

type IRepository interface {
	Store(ctx context.Context, rate *Rate) error
	FindLatestOnOrBefore(ctx context.Context, date time.Time) (*Rate, error)
	Exists(ctx context.Context, date time.Time) (bool, error)
}

type Service struct {
	repository IRepository
}

func NewService(repository IRepository) *Service {
	return &Service{repository: repository}
}

func (s *Service) Store(ctx context.Context, rate *Rate) error {
	return s.repository.Store(ctx, rate)
}

func (s *Service) RateFor(ctx context.Context, date time.Time) (*Rate, error) {
	return s.repository.FindLatestOnOrBefore(ctx, date)
}

func (s *Service) RateBefore(ctx context.Context, date time.Time) (*Rate, error) {
	return s.repository.FindLatestOnOrBefore(ctx, Day(date).AddDate(0, 0, -1))
}

func (s *Service) Exists(ctx context.Context, date time.Time) (bool, error) {
	return s.repository.Exists(ctx, date)
}
