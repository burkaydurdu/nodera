package billingstatus

import (
	"context"
	"time"

	"github.com/google/uuid"
)

//go:generate mockgen -destination=../../mock/services/billingstatus.go -package=mockservices -mock_names=IService=MockIBillingStatusService github.com/piggystats/nodera/contexts/billing/aggregates/billingstatus IService
type IService interface {
	// Find answers the Team's status, the default when the Team has no row.
	Find(ctx context.Context, teamID uuid.UUID) (*TeamStatus, error)
	// Transition moves the status when `from` admits the current one, inside
	// the caller's transaction, and answers the moved row, nil when nothing
	// changed; the caller publishes TeamBillingStatusChanged with the row's
	// ChangedAt after commit. An empty `from` admits any current status.
	Transition(ctx context.Context, teamID uuid.UUID, from []Status, to Status, at time.Time) (*TeamStatus, error)
	// FindForUpdate and Save are for collection, which also sets the open
	// Invoice, suspension and deletion fields.
	FindForUpdate(ctx context.Context, teamID uuid.UUID, now time.Time) (*TeamStatus, error)
	Save(ctx context.Context, s *TeamStatus) error
	// FindAll answers every Team's row, for republishing the statuses.
	FindAll(ctx context.Context) ([]*TeamStatus, error)
}

type IRepository interface {
	Find(ctx context.Context, teamID uuid.UUID) (*TeamStatus, error)
	FindForUpdate(ctx context.Context, teamID uuid.UUID, now time.Time) (*TeamStatus, error)
	Save(ctx context.Context, s *TeamStatus) error
	FindAll(ctx context.Context) ([]*TeamStatus, error)
}

type Service struct {
	repository IRepository
	now        func() time.Time
}

func NewService(repository IRepository) *Service {
	return &Service{repository: repository, now: time.Now}
}

func (s *Service) Find(ctx context.Context, teamID uuid.UUID) (*TeamStatus, error) {
	current, err := s.repository.Find(ctx, teamID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return Default(teamID, s.now().UTC()), nil
	}

	return current, nil
}

func (s *Service) Transition(ctx context.Context, teamID uuid.UUID, from []Status, to Status, at time.Time) (*TeamStatus, error) {
	current, err := s.repository.FindForUpdate(ctx, teamID, at)
	if err != nil {
		return nil, err
	}
	if current.Status == to || !admits(from, current.Status) {
		return nil, nil
	}

	current.Move(to, at)
	if to != Suspended {
		current.SuspendedAt = nil
		current.DeletionAt = nil
	}

	return current, s.repository.Save(ctx, current)
}

func (s *Service) FindAll(ctx context.Context) ([]*TeamStatus, error) {
	return s.repository.FindAll(ctx)
}

func (s *Service) FindForUpdate(ctx context.Context, teamID uuid.UUID, now time.Time) (*TeamStatus, error) {
	return s.repository.FindForUpdate(ctx, teamID, now)
}

func (s *Service) Save(ctx context.Context, st *TeamStatus) error {
	return s.repository.Save(ctx, st)
}

func admits(from []Status, current Status) bool {
	if len(from) == 0 {
		return true
	}
	for _, f := range from {
		if f == current {
			return true
		}
	}

	return false
}
