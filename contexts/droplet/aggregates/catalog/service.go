package catalog

import (
	"context"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

//go:generate mockgen -destination=../../mock/services/catalog.go -package=mockservices -mock_names=IService=MockICatalogService github.com/piggystats/nodera/contexts/droplet/aggregates/catalog IService
type IService interface {
	ListEnabledRegions(ctx context.Context) (Regions, error)
	ListRegions(ctx context.Context) (Regions, error)
	FindRegionByCode(ctx context.Context, code string) (*Region, error)
	ListEnabledImages(ctx context.Context) (Images, error)
	FindImageMapping(ctx context.Context, code string, providerID uuid.UUID) (*ImageMapping, error)
	ListEnabledPlansByRegion(ctx context.Context, regionID uuid.UUID) (Plans, error)
	ListPlans(ctx context.Context) (Plans, error)
	FindPlanForSale(ctx context.Context, code, regionCode string) (*Plan, error)
	ListMappedPlansByProvider(ctx context.Context, providerID uuid.UUID) (Plans, error)
	UpdatePlanPricing(ctx context.Context, planID uuid.UUID, hourly decimal.Decimal, soldOut bool) error
	MarkPlanSoldOut(ctx context.Context, planID uuid.UUID) error
	UpsertOffer(ctx context.Context, offer *Offer) error
	ExpireOffersExcept(ctx context.Context, providerID uuid.UUID, seen []uuid.UUID) (int64, error)
	ListUnmappedOffersByProvider(ctx context.Context, providerID uuid.UUID) (Offers, error)
	UpdateOffer(ctx context.Context, offerID uuid.UUID, hourly decimal.Decimal, stock int) error
}

type IRepository interface {
	ListEnabledRegions(ctx context.Context) (Regions, error)
	ListRegions(ctx context.Context) (Regions, error)
	ListPlans(ctx context.Context) (Plans, error)
	FindRegionByCode(ctx context.Context, code string) (*Region, error)
	ListEnabledImages(ctx context.Context) (Images, error)
	FindImageMapping(ctx context.Context, code string, providerID uuid.UUID) (*ImageMapping, error)
	ListEnabledPlansByRegion(ctx context.Context, regionID uuid.UUID) (Plans, error)
	FindPlanForSale(ctx context.Context, code, regionCode string) (*Plan, error)
	ListMappedPlansByProvider(ctx context.Context, providerID uuid.UUID) (Plans, error)
	UpdatePlanPricing(ctx context.Context, planID uuid.UUID, hourly decimal.Decimal, soldOut bool) error
	MarkPlanSoldOut(ctx context.Context, planID uuid.UUID) error
	UpsertOffer(ctx context.Context, offer *Offer) error
	ExpireOffersExcept(ctx context.Context, providerID uuid.UUID, seen []uuid.UUID) (int64, error)
	ListUnmappedOffersByProvider(ctx context.Context, providerID uuid.UUID) (Offers, error)
	UpdateOffer(ctx context.Context, offerID uuid.UUID, hourly decimal.Decimal, stock int) error
}

type Service struct {
	repository IRepository
}

func NewService(repository IRepository) *Service {
	return &Service{repository: repository}
}

func (s *Service) ListEnabledRegions(ctx context.Context) (Regions, error) {
	return s.repository.ListEnabledRegions(ctx)
}

func (s *Service) ListRegions(ctx context.Context) (Regions, error) {
	return s.repository.ListRegions(ctx)
}

func (s *Service) ListPlans(ctx context.Context) (Plans, error) {
	return s.repository.ListPlans(ctx)
}

func (s *Service) FindRegionByCode(ctx context.Context, code string) (*Region, error) {
	return s.repository.FindRegionByCode(ctx, code)
}

func (s *Service) ListEnabledImages(ctx context.Context) (Images, error) {
	return s.repository.ListEnabledImages(ctx)
}

func (s *Service) FindImageMapping(ctx context.Context, code string, providerID uuid.UUID) (*ImageMapping, error) {
	return s.repository.FindImageMapping(ctx, code, providerID)
}

func (s *Service) ListEnabledPlansByRegion(ctx context.Context, regionID uuid.UUID) (Plans, error) {
	return s.repository.ListEnabledPlansByRegion(ctx, regionID)
}

func (s *Service) FindPlanForSale(ctx context.Context, code, regionCode string) (*Plan, error) {
	return s.repository.FindPlanForSale(ctx, code, regionCode)
}

func (s *Service) ListMappedPlansByProvider(ctx context.Context, providerID uuid.UUID) (Plans, error) {
	return s.repository.ListMappedPlansByProvider(ctx, providerID)
}

func (s *Service) UpdatePlanPricing(ctx context.Context, planID uuid.UUID, hourly decimal.Decimal, soldOut bool) error {
	return s.repository.UpdatePlanPricing(ctx, planID, hourly, soldOut)
}

func (s *Service) MarkPlanSoldOut(ctx context.Context, planID uuid.UUID) error {
	return s.repository.MarkPlanSoldOut(ctx, planID)
}

func (s *Service) UpsertOffer(ctx context.Context, offer *Offer) error {
	return s.repository.UpsertOffer(ctx, offer)
}

func (s *Service) ExpireOffersExcept(ctx context.Context, providerID uuid.UUID, seen []uuid.UUID) (int64, error) {
	return s.repository.ExpireOffersExcept(ctx, providerID, seen)
}

func (s *Service) ListUnmappedOffersByProvider(ctx context.Context, providerID uuid.UUID) (Offers, error) {
	return s.repository.ListUnmappedOffersByProvider(ctx, providerID)
}

func (s *Service) UpdateOffer(ctx context.Context, offerID uuid.UUID, hourly decimal.Decimal, stock int) error {
	return s.repository.UpdateOffer(ctx, offerID, hourly, stock)
}
