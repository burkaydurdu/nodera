package message

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/piggystats/nodera/contexts/mail/paging"
)

//go:generate mockgen -destination=../../mock/services/message.go -package=mockservices -mock_names=IService=MockIMessageService github.com/piggystats/nodera/contexts/mail/aggregates/message IService
type IService interface {
	Create(ctx context.Context, m *Message) error
	Spool(ctx context.Context, entry *SpoolEntry) error
	LockIdempotencyKey(ctx context.Context, projectID uuid.UUID, key string) error
	FindByIdempotencyKey(ctx context.Context, projectID uuid.UUID, key string, since time.Time) (*Message, error)
	Find(ctx context.Context, projectID, id uuid.UUID) (*Message, error)
	FindForEvent(ctx context.Context, id uuid.UUID) (*Message, error)
	List(ctx context.Context, filter Filter) ([]*Message, *paging.Cursor, error)
	RecordEvent(ctx context.Context, e *Event) (bool, error)
	UpdateRecipientStatus(ctx context.Context, messageID uuid.UUID, position int, status EventType, at time.Time) error
	DueSpool(ctx context.Context, now time.Time, limit int) ([]uuid.UUID, error)
	ClaimSpool(ctx context.Context, messageID uuid.UUID) (*SpoolEntry, error)
	HandOver(ctx context.Context, messageID uuid.UUID, at time.Time) error
	DropSpool(ctx context.Context, messageID uuid.UUID) error
	RetrySpool(ctx context.Context, messageID uuid.UUID, pending Envelope, attempts int, next time.Time, lastError string) error
	ExpireCreatedBefore(ctx context.Context, before time.Time) (int64, error)
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

func (s *Service) Create(ctx context.Context, m *Message) error {
	return s.repository.Create(ctx, m)
}

func (s *Service) Spool(ctx context.Context, entry *SpoolEntry) error {
	return s.repository.Spool(ctx, entry)
}

func (s *Service) LockIdempotencyKey(ctx context.Context, projectID uuid.UUID, key string) error {
	return s.repository.LockIdempotencyKey(ctx, projectID, key)
}

func (s *Service) FindByIdempotencyKey(ctx context.Context, projectID uuid.UUID, key string, since time.Time) (*Message, error) {
	return s.repository.FindByIdempotencyKey(ctx, projectID, key, since)
}

func (s *Service) Find(ctx context.Context, projectID, id uuid.UUID) (*Message, error) {
	return s.repository.Find(ctx, projectID, id)
}

func (s *Service) FindForEvent(ctx context.Context, id uuid.UUID) (*Message, error) {
	return s.repository.FindForEvent(ctx, id)
}

func (s *Service) List(ctx context.Context, filter Filter) ([]*Message, *paging.Cursor, error) {
	return s.repository.List(ctx, filter)
}

func (s *Service) RecordEvent(ctx context.Context, e *Event) (bool, error) {
	return s.repository.RecordEvent(ctx, e)
}

func (s *Service) UpdateRecipientStatus(ctx context.Context, messageID uuid.UUID, position int, status EventType, at time.Time) error {
	return s.repository.UpdateRecipientStatus(ctx, messageID, position, status, at)
}

func (s *Service) DueSpool(ctx context.Context, now time.Time, limit int) ([]uuid.UUID, error) {
	return s.repository.DueSpool(ctx, now, limit)
}

func (s *Service) ClaimSpool(ctx context.Context, messageID uuid.UUID) (*SpoolEntry, error) {
	return s.repository.ClaimSpool(ctx, messageID)
}

func (s *Service) HandOver(ctx context.Context, messageID uuid.UUID, at time.Time) error {
	return s.repository.HandOver(ctx, messageID, at)
}

func (s *Service) DropSpool(ctx context.Context, messageID uuid.UUID) error {
	return s.repository.DropSpool(ctx, messageID)
}

func (s *Service) RetrySpool(ctx context.Context, messageID uuid.UUID, pending Envelope, attempts int, next time.Time, lastError string) error {
	return s.repository.RetrySpool(ctx, messageID, pending, attempts, next, lastError)
}

func (s *Service) ExpireCreatedBefore(ctx context.Context, before time.Time) (int64, error) {
	return s.repository.ExpireCreatedBefore(ctx, before)
}
