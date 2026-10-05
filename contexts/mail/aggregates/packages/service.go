package packages

import (
	"context"
	"time"

	"github.com/google/uuid"
)

//go:generate mockgen -destination=../../mock/services/packages.go -package=mockservices -mock_names=IService=MockIPackagesService github.com/piggystats/nodera/contexts/mail/aggregates/packages IService
type IService interface {
	ListCatalog(ctx context.Context) ([]*Package, error)
	FindCatalog(ctx context.Context, code string) (*Package, error)
	FindProject(ctx context.Context, projectID uuid.UUID) (*ProjectPackage, error)
	FindProjectForUpdate(ctx context.Context, projectID uuid.UUID) (*ProjectPackage, error)
	SaveProject(ctx context.Context, pp *ProjectPackage) error
	ListHeldByTeam(ctx context.Context, teamID uuid.UUID) ([]*ProjectPackage, error)
	ListRolloverDue(ctx context.Context, periodStart time.Time, limit int) ([]uuid.UUID, error)
	FindPeriod(ctx context.Context, projectID uuid.UUID, start time.Time) (*Period, error)
	UpsertPeriod(ctx context.Context, period *Period) error
	AddUsage(ctx context.Context, projectID uuid.UUID, start time.Time, n int) (before, after int, ok bool, err error)
	MarkNearLimitNotified(ctx context.Context, projectID uuid.UUID, start, at time.Time) (bool, error)
	MarkExhaustedNotified(ctx context.Context, projectID uuid.UUID, start, at time.Time) (bool, error)
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

func (s *Service) ListCatalog(ctx context.Context) ([]*Package, error) {
	return s.repository.ListCatalog(ctx)
}

func (s *Service) FindCatalog(ctx context.Context, code string) (*Package, error) {
	return s.repository.FindCatalog(ctx, code)
}

func (s *Service) FindProject(ctx context.Context, projectID uuid.UUID) (*ProjectPackage, error) {
	return s.repository.FindProject(ctx, projectID)
}

func (s *Service) FindProjectForUpdate(ctx context.Context, projectID uuid.UUID) (*ProjectPackage, error) {
	return s.repository.FindProjectForUpdate(ctx, projectID)
}

func (s *Service) SaveProject(ctx context.Context, pp *ProjectPackage) error {
	return s.repository.SaveProject(ctx, pp)
}

func (s *Service) ListHeldByTeam(ctx context.Context, teamID uuid.UUID) ([]*ProjectPackage, error) {
	return s.repository.ListHeldByTeam(ctx, teamID)
}

func (s *Service) ListRolloverDue(ctx context.Context, periodStart time.Time, limit int) ([]uuid.UUID, error) {
	return s.repository.ListRolloverDue(ctx, periodStart, limit)
}

func (s *Service) FindPeriod(ctx context.Context, projectID uuid.UUID, start time.Time) (*Period, error) {
	return s.repository.FindPeriod(ctx, projectID, start)
}

func (s *Service) UpsertPeriod(ctx context.Context, period *Period) error {
	return s.repository.UpsertPeriod(ctx, period)
}

func (s *Service) AddUsage(ctx context.Context, projectID uuid.UUID, start time.Time, n int) (int, int, bool, error) {
	return s.repository.AddUsage(ctx, projectID, start, n)
}

func (s *Service) MarkNearLimitNotified(ctx context.Context, projectID uuid.UUID, start, at time.Time) (bool, error) {
	return s.repository.MarkNearLimitNotified(ctx, projectID, start, at)
}

func (s *Service) MarkExhaustedNotified(ctx context.Context, projectID uuid.UUID, start, at time.Time) (bool, error) {
	return s.repository.MarkExhaustedNotified(ctx, projectID, start, at)
}
