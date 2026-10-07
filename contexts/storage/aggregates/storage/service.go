package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

//go:generate mockgen -destination=../../mock/services/storage.go -package=mockservices -mock_names=IService=MockIStorageService github.com/piggystats/nodera/contexts/storage/aggregates/storage IService
type IService interface {
	Reserve(ctx context.Context, s *Storage) error
	Activate(ctx context.Context, id uuid.UUID, startedAt time.Time) error
	Release(ctx context.Context, id uuid.UUID) error
	Find(ctx context.Context, projectID, id uuid.UUID) (*Storage, error)
	FindForUpdate(ctx context.Context, projectID, id uuid.UUID) (*Storage, error)
	FindByID(ctx context.Context, id uuid.UUID) (*Storage, error)
	FindByIDForUpdate(ctx context.Context, id uuid.UUID) (*Storage, error)
	ListByProject(ctx context.Context, projectID uuid.UUID) ([]*Storage, error)
	CountByProject(ctx context.Context, projectID uuid.UUID) (int, error)
	ListActiveByTeam(ctx context.Context, teamID uuid.UUID) ([]*Storage, error)
	ListActive(ctx context.Context, after uuid.UUID, limit int) ([]*Storage, error)
	ListScheduledDue(ctx context.Context, periodStart time.Time, limit int) ([]uuid.UUID, error)
	ListDeleting(ctx context.Context, limit int) ([]*Storage, error)
	ListAbandoned(ctx context.Context, before time.Time, limit int) ([]*Storage, error)
	ListOutOfSync(ctx context.Context, limit int) ([]uuid.UUID, error)
	Save(ctx context.Context, s *Storage) error
	SetTeamSuspended(ctx context.Context, teamID uuid.UUID, suspended bool) ([]uuid.UUID, error)
	MarkDeleted(ctx context.Context, id uuid.UUID, at time.Time) error
	FindAccount(ctx context.Context, projectID uuid.UUID) (*Account, error)
	SaveAccount(ctx context.Context, a *Account) error
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

func (s *Service) Reserve(ctx context.Context, st *Storage) error {
	return s.repository.Reserve(ctx, st)
}

func (s *Service) Activate(ctx context.Context, id uuid.UUID, startedAt time.Time) error {
	return s.repository.Activate(ctx, id, startedAt)
}

func (s *Service) Release(ctx context.Context, id uuid.UUID) error {
	return s.repository.Release(ctx, id)
}

func (s *Service) Find(ctx context.Context, projectID, id uuid.UUID) (*Storage, error) {
	return s.repository.Find(ctx, projectID, id)
}

func (s *Service) FindForUpdate(ctx context.Context, projectID, id uuid.UUID) (*Storage, error) {
	return s.repository.FindForUpdate(ctx, projectID, id)
}

func (s *Service) FindByID(ctx context.Context, id uuid.UUID) (*Storage, error) {
	return s.repository.FindByID(ctx, id)
}

func (s *Service) FindByIDForUpdate(ctx context.Context, id uuid.UUID) (*Storage, error) {
	return s.repository.FindByIDForUpdate(ctx, id)
}

func (s *Service) ListByProject(ctx context.Context, projectID uuid.UUID) ([]*Storage, error) {
	return s.repository.ListByProject(ctx, projectID)
}

func (s *Service) CountByProject(ctx context.Context, projectID uuid.UUID) (int, error) {
	return s.repository.CountByProject(ctx, projectID)
}

func (s *Service) ListActiveByTeam(ctx context.Context, teamID uuid.UUID) ([]*Storage, error) {
	return s.repository.ListActiveByTeam(ctx, teamID)
}

func (s *Service) ListActive(ctx context.Context, after uuid.UUID, limit int) ([]*Storage, error) {
	return s.repository.ListActive(ctx, after, limit)
}

func (s *Service) ListScheduledDue(ctx context.Context, periodStart time.Time, limit int) ([]uuid.UUID, error) {
	return s.repository.ListScheduledDue(ctx, periodStart, limit)
}

func (s *Service) ListDeleting(ctx context.Context, limit int) ([]*Storage, error) {
	return s.repository.ListDeleting(ctx, limit)
}

func (s *Service) ListAbandoned(ctx context.Context, before time.Time, limit int) ([]*Storage, error) {
	return s.repository.ListAbandoned(ctx, before, limit)
}

func (s *Service) ListOutOfSync(ctx context.Context, limit int) ([]uuid.UUID, error) {
	return s.repository.ListOutOfSync(ctx, limit)
}

func (s *Service) Save(ctx context.Context, st *Storage) error {
	return s.repository.Save(ctx, st)
}

func (s *Service) SetTeamSuspended(ctx context.Context, teamID uuid.UUID, suspended bool) ([]uuid.UUID, error) {
	return s.repository.SetTeamSuspended(ctx, teamID, suspended)
}

func (s *Service) MarkDeleted(ctx context.Context, id uuid.UUID, at time.Time) error {
	return s.repository.MarkDeleted(ctx, id, at)
}

func (s *Service) FindAccount(ctx context.Context, projectID uuid.UUID) (*Account, error) {
	return s.repository.FindAccount(ctx, projectID)
}

func (s *Service) SaveAccount(ctx context.Context, a *Account) error {
	return s.repository.SaveAccount(ctx, a)
}
