package handlers

import (
	"context"

	"github.com/piggystats/nodera/contexts/storage/aggregates/catalog"
	"github.com/piggystats/nodera/contexts/storage/application/dto"
)

// Catalog is what a Storage is chosen from.
type Catalog struct {
	catalog catalog.IService
}

func NewCatalog(catalogService catalog.IService) *Catalog {
	return &Catalog{catalog: catalogService}
}

func (h *Catalog) Regions(ctx context.Context) ([]dto.Region, error) {
	list, err := h.catalog.ListRegions(ctx)
	if err != nil {
		return nil, err
	}

	return dto.ToRegions(list), nil
}

func (h *Catalog) Packages(ctx context.Context) ([]dto.Package, error) {
	list, err := h.catalog.ListPackages(ctx)
	if err != nil {
		return nil, err
	}

	return dto.ToPackages(list), nil
}
