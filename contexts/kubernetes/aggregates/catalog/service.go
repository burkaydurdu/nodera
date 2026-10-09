package catalog

import "context"

//go:generate mockgen -destination=../../mock/services/catalog.go -package=mockservices -mock_names=IService=MockICatalogService github.com/piggystats/nodera/contexts/kubernetes/aggregates/catalog IService
type IService interface {
	UpsertRegion(ctx context.Context, r *Region) error
	UpsertPlan(ctx context.Context, p *Plan) error
	FindRegion(ctx context.Context, code string) (*Region, error)
	FindPlan(ctx context.Context, code, regionCode string) (*Plan, error)
	ListNodePlans(ctx context.Context, regionCode string) ([]*Plan, error)
	ListOfferedVersions(ctx context.Context) ([]*Version, error)
	FindOfferedVersion(ctx context.Context, version string) (*Version, error)
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

func (s *Service) UpsertRegion(ctx context.Context, r *Region) error {
	return s.repository.UpsertRegion(ctx, r)
}
func (s *Service) UpsertPlan(ctx context.Context, p *Plan) error {
	return s.repository.UpsertPlan(ctx, p)
}

func (s *Service) FindRegion(ctx context.Context, code string) (*Region, error) {
	return s.repository.FindRegion(ctx, code)
}

func (s *Service) FindPlan(ctx context.Context, code, regionCode string) (*Plan, error) {
	return s.repository.FindPlan(ctx, code, regionCode)
}

func (s *Service) ListNodePlans(ctx context.Context, regionCode string) ([]*Plan, error) {
	return s.repository.ListNodePlans(ctx, regionCode)
}

func (s *Service) ListOfferedVersions(ctx context.Context) ([]*Version, error) {
	return s.repository.ListOfferedVersions(ctx)
}

func (s *Service) FindOfferedVersion(ctx context.Context, version string) (*Version, error) {
	return s.repository.FindOfferedVersion(ctx, version)
}
