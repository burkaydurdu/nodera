package notification

import (
	"context"
	"time"

	"github.com/google/uuid"
)

//go:generate mockgen -destination=../../mock/services/notification.go -package=mockservices -mock_names=IService=MockINotificationService github.com/piggystats/nodera/contexts/notification/aggregates/notification IService
type IService interface {
	Record(ctx context.Context, n *Notification, channels []Channel) (bool, error)
	FindPending(ctx context.Context, sourceKey string) ([]Pending, error)
	MarkDelivered(ctx context.Context, id uuid.UUID, channel Channel, at time.Time) error
	MarkFailed(ctx context.Context, id uuid.UUID, channel Channel, cause string) error
	ListInbox(ctx context.Context, userID uuid.UUID, teams []uuid.UUID, before *uuid.UUID, limit int) ([]*Notification, error)
	CountUnread(ctx context.Context, userID uuid.UUID, teams []uuid.UUID) (int, error)
	MarkRead(ctx context.Context, userID uuid.UUID, teams []uuid.UUID, id uuid.UUID, at time.Time) (*Notification, error)
	MarkAllRead(ctx context.Context, userID uuid.UUID, teams []uuid.UUID, at time.Time) ([]uuid.UUID, error)
}

type IRepository interface {
	Record(ctx context.Context, n *Notification, channels []Channel) (bool, error)
	FindPending(ctx context.Context, sourceKey string) ([]Pending, error)
	MarkDelivered(ctx context.Context, id uuid.UUID, channel Channel, at time.Time) error
	MarkFailed(ctx context.Context, id uuid.UUID, channel Channel, cause string) error
	ListInbox(ctx context.Context, userID uuid.UUID, teams []uuid.UUID, before *uuid.UUID, limit int) ([]*Notification, error)
	CountUnread(ctx context.Context, userID uuid.UUID, teams []uuid.UUID) (int, error)
	MarkRead(ctx context.Context, userID uuid.UUID, teams []uuid.UUID, id uuid.UUID, at time.Time) (*Notification, error)
	MarkAllRead(ctx context.Context, userID uuid.UUID, teams []uuid.UUID, at time.Time) ([]uuid.UUID, error)
}

type Service struct {
	repository IRepository
}

func NewService(repository IRepository) *Service {
	return &Service{repository: repository}
}

func (s *Service) Record(ctx context.Context, n *Notification, channels []Channel) (bool, error) {
	return s.repository.Record(ctx, n, channels)
}

func (s *Service) FindPending(ctx context.Context, sourceKey string) ([]Pending, error) {
	return s.repository.FindPending(ctx, sourceKey)
}

func (s *Service) MarkDelivered(ctx context.Context, id uuid.UUID, channel Channel, at time.Time) error {
	return s.repository.MarkDelivered(ctx, id, channel, at)
}

func (s *Service) MarkFailed(ctx context.Context, id uuid.UUID, channel Channel, cause string) error {
	return s.repository.MarkFailed(ctx, id, channel, cause)
}

func (s *Service) ListInbox(ctx context.Context, userID uuid.UUID, teams []uuid.UUID, before *uuid.UUID, limit int) ([]*Notification, error) {
	return s.repository.ListInbox(ctx, userID, teams, before, limit)
}

func (s *Service) CountUnread(ctx context.Context, userID uuid.UUID, teams []uuid.UUID) (int, error) {
	return s.repository.CountUnread(ctx, userID, teams)
}

func (s *Service) MarkRead(ctx context.Context, userID uuid.UUID, teams []uuid.UUID, id uuid.UUID, at time.Time) (*Notification, error) {
	return s.repository.MarkRead(ctx, userID, teams, id, at)
}

func (s *Service) MarkAllRead(ctx context.Context, userID uuid.UUID, teams []uuid.UUID, at time.Time) ([]uuid.UUID, error) {
	return s.repository.MarkAllRead(ctx, userID, teams, at)
}
