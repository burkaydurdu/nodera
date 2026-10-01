package handlers

import (
	"context"

	"github.com/piggystats/nodera/contexts/droplet/aggregates/catalog"
	"github.com/piggystats/nodera/contexts/droplet/application/dto"
)

type Catalog struct {
	catalog catalog.IService
}

func NewCatalog(catalogService catalog.IService) *Catalog {
	return &Catalog{catalog: catalogService}
}

func (h *Catalog) ListRegions(ctx context.Context) ([]*dto.Region, error) {
	regions, err := h.catalog.ListEnabledRegions(ctx)
	if err != nil {
		return nil, err
	}

	return dto.ToRegions(regions), nil
}

// ListPlans answers the Plans of one Region; sold-out ones stay listed and
// flagged so the customer knows they exist.
func (h *Catalog) ListPlans(ctx context.Context, req *catalog.ListPlansRequest) ([]*dto.Plan, error) {
	region, err := h.catalog.FindRegionByCode(ctx, req.Region)
	if err != nil {
		return nil, err
	}

	plans, err := h.catalog.ListEnabledPlansByRegion(ctx, region.ID)
	if err != nil {
		return nil, err
	}

	return dto.ToPlans(plans), nil
}

func (h *Catalog) ListImages(ctx context.Context) ([]*dto.Image, error) {
	images, err := h.catalog.ListEnabledImages(ctx)
	if err != nil {
		return nil, err
	}

	return dto.ToImages(images), nil
}
