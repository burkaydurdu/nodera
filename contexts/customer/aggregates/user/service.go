package user

import (
	"context"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/customer/aggregates/user/mfa"
)

//go:generate mockgen -destination=../../mock/services/user.go -package=mockservices -mock_names=IService=MockIUserService github.com/piggystats/nodera/contexts/customer/aggregates/user IService
type IService interface {
	Create(ctx context.Context, user *User) error
	FindByID(ctx context.Context, id uuid.UUID) (*User, error)
	FindByEmail(ctx context.Context, email string) (*User, error)
	MarkEmailVerified(ctx context.Context, id uuid.UUID) error
	ClearPendingInvitation(ctx context.Context, id uuid.UUID) error
	UpdatePassword(ctx context.Context, id uuid.UUID, passwordHash string) error
	UpdateProfile(ctx context.Context, id uuid.UUID, fullName string, locale Locale, country string) error
	UpdateMFAMethod(ctx context.Context, id uuid.UUID, method mfa.Method) error
}

type IRepository interface {
	Create(ctx context.Context, user *User) error
	FindByID(ctx context.Context, id uuid.UUID) (*User, error)
	FindByEmail(ctx context.Context, email string) (*User, error)
	MarkEmailVerified(ctx context.Context, id uuid.UUID) error
	ClearPendingInvitation(ctx context.Context, id uuid.UUID) error
	UpdatePassword(ctx context.Context, id uuid.UUID, passwordHash string) error
	UpdateProfile(ctx context.Context, id uuid.UUID, fullName string, locale Locale, country string) error
	UpdateMFAMethod(ctx context.Context, id uuid.UUID, method mfa.Method) error
}

type Service struct {
	repository IRepository
}

func NewService(repository IRepository) *Service {
	return &Service{repository: repository}
}

func (s *Service) Create(ctx context.Context, user *User) error {
	return s.repository.Create(ctx, user)
}

func (s *Service) FindByID(ctx context.Context, id uuid.UUID) (*User, error) {
	return s.repository.FindByID(ctx, id)
}

func (s *Service) FindByEmail(ctx context.Context, email string) (*User, error) {
	return s.repository.FindByEmail(ctx, email)
}

func (s *Service) MarkEmailVerified(ctx context.Context, id uuid.UUID) error {
	return s.repository.MarkEmailVerified(ctx, id)
}

func (s *Service) ClearPendingInvitation(ctx context.Context, id uuid.UUID) error {
	return s.repository.ClearPendingInvitation(ctx, id)
}

func (s *Service) UpdatePassword(ctx context.Context, id uuid.UUID, passwordHash string) error {
	return s.repository.UpdatePassword(ctx, id, passwordHash)
}

func (s *Service) UpdateProfile(ctx context.Context, id uuid.UUID, fullName string, locale Locale, country string) error {
	return s.repository.UpdateProfile(ctx, id, fullName, locale, country)
}

func (s *Service) UpdateMFAMethod(ctx context.Context, id uuid.UUID, method mfa.Method) error {
	return s.repository.UpdateMFAMethod(ctx, id, method)
}
