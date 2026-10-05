package webhook

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/piggystats/nodera/contexts/mail/paging"
)

//go:generate mockgen -destination=../../mock/services/webhook.go -package=mockservices -mock_names=IService=MockIWebhookService github.com/piggystats/nodera/contexts/mail/aggregates/webhook IService
type IService interface {
	Create(ctx context.Context, w *Webhook) error
	Count(ctx context.Context, projectID uuid.UUID) (int, error)
	List(ctx context.Context, projectID uuid.UUID) ([]*Webhook, error)
	Find(ctx context.Context, projectID, id uuid.UUID) (*Webhook, error)
	Update(ctx context.Context, w *Webhook) error
	Delete(ctx context.Context, projectID, id uuid.UUID) error
	Enqueue(ctx context.Context, projectID uuid.UUID, eventType string, payload []byte, at time.Time) error
	ListWithPending(ctx context.Context, limit int) ([]uuid.UUID, error)
	Batch(ctx context.Context, webhookID uuid.UUID, limit int, at time.Time) (*Delivery, error)
	ListDue(ctx context.Context, now time.Time, limit int) ([]*DueDelivery, error)
	RecordAttempt(ctx context.Context, d *Delivery) error
	ListDeliveries(ctx context.Context, webhookID uuid.UUID, before *paging.Cursor, limit int) ([]*Delivery, *paging.Cursor, error)
	ExpireBefore(ctx context.Context, before time.Time) error
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

func (s *Service) Create(ctx context.Context, w *Webhook) error {
	return s.repository.Create(ctx, w)
}

func (s *Service) Count(ctx context.Context, projectID uuid.UUID) (int, error) {
	return s.repository.Count(ctx, projectID)
}

func (s *Service) List(ctx context.Context, projectID uuid.UUID) ([]*Webhook, error) {
	return s.repository.List(ctx, projectID)
}

func (s *Service) Find(ctx context.Context, projectID, id uuid.UUID) (*Webhook, error) {
	return s.repository.Find(ctx, projectID, id)
}

func (s *Service) Update(ctx context.Context, w *Webhook) error {
	return s.repository.Update(ctx, w)
}

func (s *Service) Delete(ctx context.Context, projectID, id uuid.UUID) error {
	return s.repository.Delete(ctx, projectID, id)
}

func (s *Service) Enqueue(ctx context.Context, projectID uuid.UUID, eventType string, payload []byte, at time.Time) error {
	return s.repository.Enqueue(ctx, projectID, eventType, payload, at)
}

func (s *Service) ListWithPending(ctx context.Context, limit int) ([]uuid.UUID, error) {
	return s.repository.ListWithPending(ctx, limit)
}

func (s *Service) Batch(ctx context.Context, webhookID uuid.UUID, limit int, at time.Time) (*Delivery, error) {
	return s.repository.Batch(ctx, webhookID, limit, at)
}

func (s *Service) ListDue(ctx context.Context, now time.Time, limit int) ([]*DueDelivery, error) {
	return s.repository.ListDue(ctx, now, limit)
}

func (s *Service) RecordAttempt(ctx context.Context, d *Delivery) error {
	return s.repository.RecordAttempt(ctx, d)
}

func (s *Service) ListDeliveries(ctx context.Context, webhookID uuid.UUID, before *paging.Cursor, limit int) ([]*Delivery, *paging.Cursor, error) {
	return s.repository.ListDeliveries(ctx, webhookID, before, limit)
}

func (s *Service) ExpireBefore(ctx context.Context, before time.Time) error {
	return s.repository.ExpireBefore(ctx, before)
}
