package sendingdomain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

//go:generate mockgen -destination=../../mock/services/sending_domain.go -package=mockservices -mock_names=IService=MockISendingDomainService github.com/piggystats/nodera/contexts/mail/aggregates/sendingdomain IService
type IService interface {
	Create(ctx context.Context, d *SendingDomain, keys []*Key) error
	CountByProject(ctx context.Context, projectID uuid.UUID) (int, error)
	Find(ctx context.Context, projectID, id uuid.UUID) (*SendingDomain, error)
	FindForUpdate(ctx context.Context, id uuid.UUID) (*SendingDomain, error)
	FindByID(ctx context.Context, id uuid.UUID) (*SendingDomain, error)
	ListByProject(ctx context.Context, projectID uuid.UUID) (SendingDomains, error)
	FindVerifiedByName(ctx context.Context, projectID uuid.UUID, name string) (*SendingDomain, error)
	Save(ctx context.Context, d *SendingDomain) error
	Delete(ctx context.Context, projectID, id uuid.UUID) error
	ListDue(ctx context.Context, now time.Time, limit int) (SendingDomains, error)
	ListRotationDue(ctx context.Context, rotatedBefore time.Time, limit int) (SendingDomains, error)
	ListRotationReady(ctx context.Context, now time.Time, limit int) (SendingDomains, error)
	FindKey(ctx context.Context, domainID uuid.UUID, selector Selector) (*Key, error)
	ReplaceKey(ctx context.Context, key *Key) error
	ListPublishedKeys(ctx context.Context) ([]*PublishedKey, error)
	FindPublishedKey(ctx context.Context, token string, selector Selector) (*PublishedKey, error)
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

func (s *Service) Create(ctx context.Context, d *SendingDomain, keys []*Key) error {
	return s.repository.Create(ctx, d, keys)
}

func (s *Service) CountByProject(ctx context.Context, projectID uuid.UUID) (int, error) {
	return s.repository.CountByProject(ctx, projectID)
}

func (s *Service) Find(ctx context.Context, projectID, id uuid.UUID) (*SendingDomain, error) {
	return s.repository.Find(ctx, projectID, id)
}

func (s *Service) FindForUpdate(ctx context.Context, id uuid.UUID) (*SendingDomain, error) {
	return s.repository.FindForUpdate(ctx, id)
}

func (s *Service) FindByID(ctx context.Context, id uuid.UUID) (*SendingDomain, error) {
	return s.repository.FindByID(ctx, id)
}

func (s *Service) ListByProject(ctx context.Context, projectID uuid.UUID) (SendingDomains, error) {
	return s.repository.ListByProject(ctx, projectID)
}

func (s *Service) FindVerifiedByName(ctx context.Context, projectID uuid.UUID, name string) (*SendingDomain, error) {
	return s.repository.FindVerifiedByName(ctx, projectID, name)
}

func (s *Service) Save(ctx context.Context, d *SendingDomain) error {
	return s.repository.Save(ctx, d)
}

func (s *Service) Delete(ctx context.Context, projectID, id uuid.UUID) error {
	return s.repository.Delete(ctx, projectID, id)
}

func (s *Service) ListDue(ctx context.Context, now time.Time, limit int) (SendingDomains, error) {
	return s.repository.ListDue(ctx, now, limit)
}

func (s *Service) ListRotationDue(ctx context.Context, rotatedBefore time.Time, limit int) (SendingDomains, error) {
	return s.repository.ListRotationDue(ctx, rotatedBefore, limit)
}

func (s *Service) ListRotationReady(ctx context.Context, now time.Time, limit int) (SendingDomains, error) {
	return s.repository.ListRotationReady(ctx, now, limit)
}

func (s *Service) FindKey(ctx context.Context, domainID uuid.UUID, selector Selector) (*Key, error) {
	return s.repository.FindKey(ctx, domainID, selector)
}

func (s *Service) ReplaceKey(ctx context.Context, key *Key) error {
	return s.repository.ReplaceKey(ctx, key)
}

func (s *Service) ListPublishedKeys(ctx context.Context) ([]*PublishedKey, error) {
	return s.repository.ListPublishedKeys(ctx)
}

func (s *Service) FindPublishedKey(ctx context.Context, token string, selector Selector) (*PublishedKey, error) {
	return s.repository.FindPublishedKey(ctx, token, selector)
}
