package suppression

import (
	"context"

	"github.com/google/uuid"
	"github.com/piggystats/nodera/contexts/mail/paging"
)

//go:generate mockgen -destination=../../mock/services/suppression.go -package=mockservices -mock_names=IService=MockISuppressionService github.com/piggystats/nodera/contexts/mail/aggregates/suppression IService
type IService interface {
	Add(ctx context.Context, s *Suppression) error
	Suppressed(ctx context.Context, projectID uuid.UUID, addresses []string) (map[string]bool, error)
	List(ctx context.Context, projectID uuid.UUID, search string, before *paging.Cursor, limit int) ([]*Suppression, *paging.Cursor, error)
	Find(ctx context.Context, projectID, id uuid.UUID) (*Suppression, error)
	Delete(ctx context.Context, projectID, id uuid.UUID) error
}

type IRepository interface {
	IService
}

type Service struct {
	repository IRepository
}

func NewService(repository IRepository) *Service {
	return &Service{repository: repository}
}

func (s *Service) Add(ctx context.Context, sup *Suppression) error {
	return s.repository.Add(ctx, sup)
}

func (s *Service) Suppressed(ctx context.Context, projectID uuid.UUID, addresses []string) (map[string]bool, error) {
	return s.repository.Suppressed(ctx, projectID, addresses)
}

func (s *Service) List(ctx context.Context, projectID uuid.UUID, search string, before *paging.Cursor, limit int) ([]*Suppression, *paging.Cursor, error) {
	return s.repository.List(ctx, projectID, search, before, limit)
}

func (s *Service) Find(ctx context.Context, projectID, id uuid.UUID) (*Suppression, error) {
	return s.repository.Find(ctx, projectID, id)
}

func (s *Service) Delete(ctx context.Context, projectID, id uuid.UUID) error {
	return s.repository.Delete(ctx, projectID, id)
}
