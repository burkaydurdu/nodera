package payment

import (
	"context"
	"time"

	"github.com/google/uuid"
)

//go:generate mockgen -destination=../../mock/services/payment.go -package=mockservices -mock_names=IService=MockIPaymentService github.com/piggystats/nodera/contexts/billing/aggregates/payment IService
type IService interface {
	StoreWebhookEvent(ctx context.Context, e *WebhookEvent) (bool, error)
	FindUnprocessedWebhookEvents(ctx context.Context, limit int) ([]*WebhookEvent, error)
	MarkWebhookEventProcessed(ctx context.Context, e *WebhookEvent) error

	// NextAttempt answers the attempt number the Invoice's next Payment takes.
	NextAttempt(ctx context.Context, invoiceID uuid.UUID) (int, error)
	Create(ctx context.Context, p *Payment) error
	Save(ctx context.Context, p *Payment) error
	FindByID(ctx context.Context, id uuid.UUID) (*Payment, error)
	// FindForUpdate locks the Payment so one outcome is applied once.
	FindForUpdate(ctx context.Context, id uuid.UUID) (*Payment, error)
	FindByInvoiceID(ctx context.Context, invoiceID uuid.UUID) ([]*Payment, error)
	FindByProviderRef(ctx context.Context, provider, ref string) (*Payment, error)
	FindByIdempotencyKey(ctx context.Context, key string) (*Payment, error)
	// FindPending answers attempts still waiting for the provider's answer
	// since before the instant, oldest first.
	FindPending(ctx context.Context, before time.Time, limit int) ([]*Payment, error)
	// FindExpiredConfirmations answers confirmation_required attempts whose
	// link lapsed by the instant.
	FindExpiredConfirmations(ctx context.Context, at time.Time, limit int) ([]*Payment, error)
}

type IRepository interface {
	StoreWebhookEvent(ctx context.Context, e *WebhookEvent) (bool, error)
	FindUnprocessedWebhookEvents(ctx context.Context, limit int) ([]*WebhookEvent, error)
	MarkWebhookEventProcessed(ctx context.Context, e *WebhookEvent) error
	NextAttempt(ctx context.Context, invoiceID uuid.UUID) (int, error)
	Create(ctx context.Context, p *Payment) error
	Save(ctx context.Context, p *Payment) error
	FindByID(ctx context.Context, id uuid.UUID, forUpdate bool) (*Payment, error)
	FindByInvoiceID(ctx context.Context, invoiceID uuid.UUID) ([]*Payment, error)
	FindByProviderRef(ctx context.Context, provider, ref string) (*Payment, error)
	FindByIdempotencyKey(ctx context.Context, key string) (*Payment, error)
	FindPending(ctx context.Context, before time.Time, limit int) ([]*Payment, error)
	FindExpiredConfirmations(ctx context.Context, at time.Time, limit int) ([]*Payment, error)
}

type Service struct {
	repository IRepository
}

func NewService(repository IRepository) *Service {
	return &Service{repository: repository}
}

func (s *Service) StoreWebhookEvent(ctx context.Context, e *WebhookEvent) (bool, error) {
	return s.repository.StoreWebhookEvent(ctx, e)
}

func (s *Service) FindUnprocessedWebhookEvents(ctx context.Context, limit int) ([]*WebhookEvent, error) {
	return s.repository.FindUnprocessedWebhookEvents(ctx, limit)
}

func (s *Service) MarkWebhookEventProcessed(ctx context.Context, e *WebhookEvent) error {
	return s.repository.MarkWebhookEventProcessed(ctx, e)
}

func (s *Service) NextAttempt(ctx context.Context, invoiceID uuid.UUID) (int, error) {
	return s.repository.NextAttempt(ctx, invoiceID)
}

// Create derives the idempotency key from the Invoice and attempt.
func (s *Service) Create(ctx context.Context, p *Payment) error {
	p.IdempotencyKey = IdempotencyKey(p.InvoiceID, p.Attempt)

	return s.repository.Create(ctx, p)
}

func (s *Service) Save(ctx context.Context, p *Payment) error {
	return s.repository.Save(ctx, p)
}

func (s *Service) FindByID(ctx context.Context, id uuid.UUID) (*Payment, error) {
	return s.repository.FindByID(ctx, id, false)
}

func (s *Service) FindForUpdate(ctx context.Context, id uuid.UUID) (*Payment, error) {
	return s.repository.FindByID(ctx, id, true)
}

func (s *Service) FindByInvoiceID(ctx context.Context, invoiceID uuid.UUID) ([]*Payment, error) {
	return s.repository.FindByInvoiceID(ctx, invoiceID)
}

func (s *Service) FindByProviderRef(ctx context.Context, provider, ref string) (*Payment, error) {
	return s.repository.FindByProviderRef(ctx, provider, ref)
}

func (s *Service) FindByIdempotencyKey(ctx context.Context, key string) (*Payment, error) {
	return s.repository.FindByIdempotencyKey(ctx, key)
}

func (s *Service) FindPending(ctx context.Context, before time.Time, limit int) ([]*Payment, error) {
	return s.repository.FindPending(ctx, before, limit)
}

func (s *Service) FindExpiredConfirmations(ctx context.Context, at time.Time, limit int) ([]*Payment, error) {
	return s.repository.FindExpiredConfirmations(ctx, at, limit)
}
