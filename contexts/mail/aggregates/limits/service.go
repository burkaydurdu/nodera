package limits

import (
	"context"
	"time"

	"github.com/google/uuid"
)

//go:generate mockgen -destination=../../mock/services/limits.go -package=mockservices -mock_names=IService=MockILimitsService github.com/piggystats/nodera/contexts/mail/aggregates/limits IService
type IService interface {
	Ensure(ctx context.Context, projectID, teamID uuid.UUID) (*Limits, error)
	Find(ctx context.Context, projectID uuid.UUID) (*Limits, error)
	ConsumeDaily(ctx context.Context, projectID uuid.UUID, day time.Time, limit, n int) (bool, error)
	FindDay(ctx context.Context, projectID uuid.UUID, day time.Time) (*Day, error)
	AddOutcome(ctx context.Context, projectID uuid.UUID, day time.Time, outcome Outcome, n int) error
	Window(ctx context.Context, projectID uuid.UUID, from, to time.Time) (Window, error)
	ListActiveSince(ctx context.Context, from time.Time) ([]*Limits, error)
	ListUnevaluated(ctx context.Context, before time.Time, limit int) ([]*Day, error)
	MarkEvaluated(ctx context.Context, projectID uuid.UUID, day, at time.Time) (bool, error)
	DoubleDailyLimit(ctx context.Context, projectID uuid.UUID) error
	Pause(ctx context.Context, projectID uuid.UUID, reason Reason, at time.Time) (bool, error)
	Unpause(ctx context.Context, projectID uuid.UUID, at time.Time) (bool, error)
	MarkRatesWarned(ctx context.Context, projectID uuid.UUID, at time.Time) (bool, error)
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

func (s *Service) Ensure(ctx context.Context, projectID, teamID uuid.UUID) (*Limits, error) {
	return s.repository.Ensure(ctx, projectID, teamID)
}

func (s *Service) Find(ctx context.Context, projectID uuid.UUID) (*Limits, error) {
	return s.repository.Find(ctx, projectID)
}

func (s *Service) ConsumeDaily(ctx context.Context, projectID uuid.UUID, day time.Time, limit, n int) (bool, error) {
	return s.repository.ConsumeDaily(ctx, projectID, day, limit, n)
}

func (s *Service) FindDay(ctx context.Context, projectID uuid.UUID, day time.Time) (*Day, error) {
	return s.repository.FindDay(ctx, projectID, day)
}

func (s *Service) AddOutcome(ctx context.Context, projectID uuid.UUID, day time.Time, outcome Outcome, n int) error {
	return s.repository.AddOutcome(ctx, projectID, day, outcome, n)
}

func (s *Service) Window(ctx context.Context, projectID uuid.UUID, from, to time.Time) (Window, error) {
	return s.repository.Window(ctx, projectID, from, to)
}

func (s *Service) ListActiveSince(ctx context.Context, from time.Time) ([]*Limits, error) {
	return s.repository.ListActiveSince(ctx, from)
}

func (s *Service) ListUnevaluated(ctx context.Context, before time.Time, limit int) ([]*Day, error) {
	return s.repository.ListUnevaluated(ctx, before, limit)
}

func (s *Service) MarkEvaluated(ctx context.Context, projectID uuid.UUID, day, at time.Time) (bool, error) {
	return s.repository.MarkEvaluated(ctx, projectID, day, at)
}

func (s *Service) DoubleDailyLimit(ctx context.Context, projectID uuid.UUID) error {
	return s.repository.DoubleDailyLimit(ctx, projectID)
}

func (s *Service) Pause(ctx context.Context, projectID uuid.UUID, reason Reason, at time.Time) (bool, error) {
	return s.repository.Pause(ctx, projectID, reason, at)
}

func (s *Service) Unpause(ctx context.Context, projectID uuid.UUID, at time.Time) (bool, error) {
	return s.repository.Unpause(ctx, projectID, at)
}

func (s *Service) MarkRatesWarned(ctx context.Context, projectID uuid.UUID, at time.Time) (bool, error) {
	return s.repository.MarkRatesWarned(ctx, projectID, at)
}
