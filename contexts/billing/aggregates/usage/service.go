package usage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

//go:generate mockgen -destination=../../mock/services/usage.go -package=mockservices -mock_names=IService=MockIUsageService github.com/piggystats/nodera/contexts/billing/aggregates/usage IService
type IService interface {
	// Start records a Droplet's start; Stop its stop. Both are idempotent and
	// tolerate arriving in either order.
	Start(ctx context.Context, u *Usage) error
	Stop(ctx context.Context, dropletID, teamID uuid.UUID, stoppedAt time.Time) error
	// StartMail records the start of a Package interval and StopMail its
	// stop, both naming it by the Project and its start. Both are idempotent
	// and tolerate arriving in either order.
	StartMail(ctx context.Context, i *MailInterval) error
	StopMail(ctx context.Context, projectID, teamID uuid.UUID, startedAt, stoppedAt time.Time) error
	// FindForTeam answers the Team's Droplets that ran at some point in
	// [from, to), FindMailForTeam its Package intervals held at some point in
	// it; TeamsWithUsage the Teams that have either.
	FindForTeam(ctx context.Context, teamID uuid.UUID, from, to time.Time) ([]*Usage, error)
	FindMailForTeam(ctx context.Context, teamID uuid.UUID, from, to time.Time) ([]*MailInterval, error)
	TeamsWithUsage(ctx context.Context, from, to time.Time) ([]uuid.UUID, error)
	// FirstStartedAt answers when the Team's first Droplet started; nil
	// without one.
	FirstStartedAt(ctx context.Context, teamID uuid.UUID) (*time.Time, error)
}

type IRepository interface {
	Start(ctx context.Context, u *Usage) error
	Stop(ctx context.Context, dropletID, teamID uuid.UUID, stoppedAt time.Time) error
	StartMail(ctx context.Context, i *MailInterval) error
	StopMail(ctx context.Context, projectID, teamID uuid.UUID, startedAt, stoppedAt time.Time) error
	FindForTeam(ctx context.Context, teamID uuid.UUID, from, to time.Time) ([]*Usage, error)
	FindMailForTeam(ctx context.Context, teamID uuid.UUID, from, to time.Time) ([]*MailInterval, error)
	TeamsWithUsage(ctx context.Context, from, to time.Time) ([]uuid.UUID, error)
	FirstStartedAt(ctx context.Context, teamID uuid.UUID) (*time.Time, error)
}

type Service struct {
	repository IRepository
}

func NewService(repository IRepository) *Service {
	return &Service{repository: repository}
}

func (s *Service) Start(ctx context.Context, u *Usage) error {
	return s.repository.Start(ctx, u)
}

func (s *Service) Stop(ctx context.Context, dropletID, teamID uuid.UUID, stoppedAt time.Time) error {
	return s.repository.Stop(ctx, dropletID, teamID, stoppedAt)
}

func (s *Service) StartMail(ctx context.Context, i *MailInterval) error {
	return s.repository.StartMail(ctx, i)
}

func (s *Service) StopMail(ctx context.Context, projectID, teamID uuid.UUID, startedAt, stoppedAt time.Time) error {
	return s.repository.StopMail(ctx, projectID, teamID, startedAt, stoppedAt)
}

func (s *Service) FindForTeam(ctx context.Context, teamID uuid.UUID, from, to time.Time) ([]*Usage, error) {
	return s.repository.FindForTeam(ctx, teamID, from, to)
}

func (s *Service) FindMailForTeam(ctx context.Context, teamID uuid.UUID, from, to time.Time) ([]*MailInterval, error) {
	return s.repository.FindMailForTeam(ctx, teamID, from, to)
}

func (s *Service) TeamsWithUsage(ctx context.Context, from, to time.Time) ([]uuid.UUID, error) {
	return s.repository.TeamsWithUsage(ctx, from, to)
}

func (s *Service) FirstStartedAt(ctx context.Context, teamID uuid.UUID) (*time.Time, error) {
	return s.repository.FirstStartedAt(ctx, teamID)
}
