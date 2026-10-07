package catalog

import "context"

//go:generate mockgen -destination=../../mock/services/catalog.go -package=mockservices -mock_names=IService=MockICatalogService github.com/piggystats/nodera/contexts/storage/aggregates/catalog IService
type IService interface {
	ListRegions(ctx context.Context) ([]*Region, error)
	FindRegion(ctx context.Context, code string) (*Region, error)
	ListPackages(ctx context.Context) ([]*Package, error)
	FindPackage(ctx context.Context, code string) (*Package, error)
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

func (s *Service) ListRegions(ctx context.Context) ([]*Region, error) {
	return s.repository.ListRegions(ctx)
}

func (s *Service) FindRegion(ctx context.Context, code string) (*Region, error) {
	return s.repository.FindRegion(ctx, code)
}

func (s *Service) ListPackages(ctx context.Context) ([]*Package, error) {
	return s.repository.ListPackages(ctx)
}

func (s *Service) FindPackage(ctx context.Context, code string) (*Package, error) {
	return s.repository.FindPackage(ctx, code)
}
