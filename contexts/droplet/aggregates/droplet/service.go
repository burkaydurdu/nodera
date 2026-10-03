package droplet

import (
	"context"

	"github.com/google/uuid"
)

//go:generate mockgen -destination=../../mock/services/droplet.go -package=mockservices -mock_names=IService=MockIDropletService github.com/piggystats/nodera/contexts/droplet/aggregates/droplet IService
type IService interface {
	Create(ctx context.Context, d *Droplet) error
	FindByID(ctx context.Context, id uuid.UUID) (*Droplet, error)
	FindByIDForUpdate(ctx context.Context, id uuid.UUID) (*Droplet, error)
	FindByProjectID(ctx context.Context, projectID uuid.UUID) (Droplets, error)
	FindByTeamID(ctx context.Context, teamID uuid.UUID) (Droplets, error)
	FindTransitional(ctx context.Context) (Droplets, error)
	FindBilled(ctx context.Context) (Droplets, error)
	Save(ctx context.Context, d *Droplet) error
	Transition(ctx context.Context, d *Droplet, to Status, reason string) error
	Fail(ctx context.Context, d *Droplet, code FailureCode, reason string) error
	ListHistory(ctx context.Context, dropletID uuid.UUID) (History, error)
}

type IRepository interface {
	Create(ctx context.Context, d *Droplet) error
	FindByID(ctx context.Context, id uuid.UUID) (*Droplet, error)
	FindByIDForUpdate(ctx context.Context, id uuid.UUID) (*Droplet, error)
	FindByProjectID(ctx context.Context, projectID uuid.UUID) (Droplets, error)
	FindByTeamID(ctx context.Context, teamID uuid.UUID) (Droplets, error)
	FindTransitional(ctx context.Context) (Droplets, error)
	FindBilled(ctx context.Context) (Droplets, error)
	Save(ctx context.Context, d *Droplet) error
	AddHistory(ctx context.Context, dropletID uuid.UUID, entry *HistoryEntry) error
	ListHistory(ctx context.Context, dropletID uuid.UUID) (History, error)
}

type Service struct {
	repository IRepository
}

func NewService(repository IRepository) *Service {
	return &Service{repository: repository}
}

func (s *Service) Create(ctx context.Context, d *Droplet) error {
	return s.repository.Create(ctx, d)
}

func (s *Service) FindByID(ctx context.Context, id uuid.UUID) (*Droplet, error) {
	return s.repository.FindByID(ctx, id)
}

func (s *Service) FindByIDForUpdate(ctx context.Context, id uuid.UUID) (*Droplet, error) {
	return s.repository.FindByIDForUpdate(ctx, id)
}

func (s *Service) FindByProjectID(ctx context.Context, projectID uuid.UUID) (Droplets, error) {
	return s.repository.FindByProjectID(ctx, projectID)
}

func (s *Service) FindByTeamID(ctx context.Context, teamID uuid.UUID) (Droplets, error) {
	return s.repository.FindByTeamID(ctx, teamID)
}

func (s *Service) FindTransitional(ctx context.Context) (Droplets, error) {
	return s.repository.FindTransitional(ctx)
}

func (s *Service) FindBilled(ctx context.Context) (Droplets, error) {
	return s.repository.FindBilled(ctx)
}

func (s *Service) Save(ctx context.Context, d *Droplet) error {
	return s.repository.Save(ctx, d)
}

// Transition applies the status table, persists the Droplet and records the
// history row; the caller decides whether a transaction surrounds it.
func (s *Service) Transition(ctx context.Context, d *Droplet, to Status, reason string) error {
	entry, err := d.Transition(to, reason, Now())
	if err != nil {
		return err
	}
	if err := s.repository.Save(ctx, d); err != nil {
		return err
	}

	return s.repository.AddHistory(ctx, d.ID, entry)
}

func (s *Service) Fail(ctx context.Context, d *Droplet, code FailureCode, reason string) error {
	entry, err := d.Fail(code, reason, Now())
	if err != nil {
		return err
	}
	if err := s.repository.Save(ctx, d); err != nil {
		return err
	}

	return s.repository.AddHistory(ctx, d.ID, entry)
}

func (s *Service) ListHistory(ctx context.Context, dropletID uuid.UUID) (History, error) {
	return s.repository.ListHistory(ctx, dropletID)
}
