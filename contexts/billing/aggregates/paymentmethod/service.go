package paymentmethod

import (
	"context"
	"time"

	"github.com/google/uuid"
)

//go:generate mockgen -destination=../../mock/services/paymentmethod.go -package=mockservices -mock_names=IService=MockIPaymentMethodService github.com/piggystats/nodera/contexts/billing/aggregates/paymentmethod IService
type IService interface {
	Create(ctx context.Context, m *PaymentMethod) error
	UpdateExpiry(ctx context.Context, id uuid.UUID, month, year int) error
	FindByID(ctx context.Context, teamID, id uuid.UUID) (*PaymentMethod, error)
	FindAnyByID(ctx context.Context, teamID, id uuid.UUID) (*PaymentMethod, error)
	FindUnwarnedDefaults(ctx context.Context) (PaymentMethods, error)
	MarkExpiryNotified(ctx context.Context, id uuid.UUID, at time.Time) (bool, error)
	FindByTeamID(ctx context.Context, teamID uuid.UUID) (PaymentMethods, error)
	CountByTeamID(ctx context.Context, teamID uuid.UUID) (int, error)
	SetDefault(ctx context.Context, teamID, id uuid.UUID) error
	Delete(ctx context.Context, teamID, id uuid.UUID, at time.Time) error
	CreateVerification(ctx context.Context, v *Verification) error
	FindVerification(ctx context.Context, teamID, id uuid.UUID) (*Verification, error)
	SaveVerification(ctx context.Context, v *Verification) error
	FindRoute(ctx context.Context, country string) (*Route, error)
}

type IRepository interface {
	Create(ctx context.Context, m *PaymentMethod) error
	UpdateExpiry(ctx context.Context, id uuid.UUID, month, year int) error
	FindByID(ctx context.Context, teamID, id uuid.UUID) (*PaymentMethod, error)
	FindAnyByID(ctx context.Context, teamID, id uuid.UUID) (*PaymentMethod, error)
	FindUnwarnedDefaults(ctx context.Context) (PaymentMethods, error)
	MarkExpiryNotified(ctx context.Context, id uuid.UUID, at time.Time) (bool, error)
	FindByTeamID(ctx context.Context, teamID uuid.UUID) (PaymentMethods, error)
	CountByTeamID(ctx context.Context, teamID uuid.UUID) (int, error)
	SetDefault(ctx context.Context, teamID, id uuid.UUID) error
	Delete(ctx context.Context, teamID, id uuid.UUID, at time.Time) error
	CreateVerification(ctx context.Context, v *Verification) error
	FindVerification(ctx context.Context, teamID, id uuid.UUID) (*Verification, error)
	SaveVerification(ctx context.Context, v *Verification) error
	FindRoute(ctx context.Context, country string) (*Route, error)
}

type Service struct {
	repository IRepository
}

func NewService(repository IRepository) *Service {
	return &Service{repository: repository}
}

func (s *Service) Create(ctx context.Context, m *PaymentMethod) error {
	return s.repository.Create(ctx, m)
}

func (s *Service) FindByID(ctx context.Context, teamID, id uuid.UUID) (*PaymentMethod, error) {
	return s.repository.FindByID(ctx, teamID, id)
}

func (s *Service) FindByTeamID(ctx context.Context, teamID uuid.UUID) (PaymentMethods, error) {
	return s.repository.FindByTeamID(ctx, teamID)
}

func (s *Service) CountByTeamID(ctx context.Context, teamID uuid.UUID) (int, error) {
	return s.repository.CountByTeamID(ctx, teamID)
}

func (s *Service) SetDefault(ctx context.Context, teamID, id uuid.UUID) error {
	return s.repository.SetDefault(ctx, teamID, id)
}

func (s *Service) Delete(ctx context.Context, teamID, id uuid.UUID, at time.Time) error {
	return s.repository.Delete(ctx, teamID, id, at)
}

func (s *Service) CreateVerification(ctx context.Context, v *Verification) error {
	return s.repository.CreateVerification(ctx, v)
}

func (s *Service) FindVerification(ctx context.Context, teamID, id uuid.UUID) (*Verification, error) {
	return s.repository.FindVerification(ctx, teamID, id)
}

func (s *Service) SaveVerification(ctx context.Context, v *Verification) error {
	return s.repository.SaveVerification(ctx, v)
}

func (s *Service) FindRoute(ctx context.Context, country string) (*Route, error) {
	return s.repository.FindRoute(ctx, country)
}

func (s *Service) FindAnyByID(ctx context.Context, teamID, id uuid.UUID) (*PaymentMethod, error) {
	return s.repository.FindAnyByID(ctx, teamID, id)
}

func (s *Service) FindUnwarnedDefaults(ctx context.Context) (PaymentMethods, error) {
	return s.repository.FindUnwarnedDefaults(ctx)
}

func (s *Service) MarkExpiryNotified(ctx context.Context, id uuid.UUID, at time.Time) (bool, error) {
	return s.repository.MarkExpiryNotified(ctx, id, at)
}

func (s *Service) UpdateExpiry(ctx context.Context, id uuid.UUID, month, year int) error {
	return s.repository.UpdateExpiry(ctx, id, month, year)
}
