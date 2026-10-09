package handlers

import (
	"context"
	"errors"

	"github.com/piggystats/nodera/contexts/kubernetes/aggregates/catalog"
	"github.com/piggystats/nodera/contexts/kubernetes/application/dto"
)

// Catalog answers what a Cluster can be made of: the versions offered, the
// Regions Kubernetes is sold in and the Plans a Node may have there.
type Catalog struct {
	catalog  catalog.IService
	settings Settings
}

func NewCatalog(catalogService catalog.IService, settings Settings) *Catalog {
	return &Catalog{catalog: catalogService, settings: settings}
}

func (h *Catalog) ListVersions(ctx context.Context) ([]dto.Version, error) {
	versions, err := h.catalog.ListOfferedVersions(ctx)
	if err != nil {
		return nil, err
	}

	return dto.ToVersions(versions), nil
}

// ListRegions is the Regions with a control plane fleet: one at first
// (docs/adr/0013), and only while the droplet catalog sells it.
func (h *Catalog) ListRegions(ctx context.Context) ([]dto.Region, error) {
	region, err := h.catalog.FindRegion(ctx, h.settings.Region)
	if errors.Is(err, catalog.ErrorRegionNotFound) {
		return []dto.Region{}, nil
	}
	if err != nil {
		return nil, err
	}

	return dto.ToRegions([]*catalog.Region{region}), nil
}

func (h *Catalog) ListPlans(ctx context.Context, req *catalog.ListPlansRequest) ([]dto.Plan, error) {
	req.Normalize()
	region, err := h.catalog.FindRegion(ctx, req.Region)
	if err != nil {
		return nil, err
	}
	if region.Code != h.settings.Region {
		return nil, catalog.ErrorRegionNotSold
	}
	plans, err := h.catalog.ListNodePlans(ctx, region.Code)
	if err != nil {
		return nil, err
	}

	return dto.ToPlans(plans), nil
}
