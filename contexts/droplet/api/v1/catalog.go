package v1

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/contexts/droplet/aggregates/catalog"
	"github.com/piggystats/nodera/contexts/droplet/api/v1/middleware"
	"github.com/piggystats/nodera/contexts/droplet/application/dto"
)

type ICatalogHandler interface {
	ListRegions(ctx context.Context) ([]*dto.Region, error)
	ListPlans(ctx context.Context, req *catalog.ListPlansRequest) ([]*dto.Plan, error)
	ListImages(ctx context.Context) ([]*dto.Image, error)
}

type Catalog struct {
	handler      ICatalogHandler
	authenticate echo.MiddlewareFunc
}

func NewCatalog(handler ICatalogHandler, authenticate echo.MiddlewareFunc) *Catalog {
	return &Catalog{handler: handler, authenticate: authenticate}
}

// The catalog needs a session but no Team: prices are visible to any
// signed-in customer.
func (h *Catalog) SetRoutes(group *echo.Group) {
	routes := group.Group("/catalog", h.authenticate)
	routes.GET("/regions", h.ListRegions)
	routes.GET("/plans", h.ListPlans)
	routes.GET("/images", h.ListImages)
}

// ListRegions godoc
// @Summary Regions Nodera sells Droplets in
// @Tags catalog
// @Security BearerAuth
// @Produce json
// @Success 200 {array} dto.Region
// @Router /v1/catalog/regions [get]
func (h *Catalog) ListRegions(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	regions, err := h.handler.ListRegions(ctx)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, regions)
}

// ListPlans godoc
// @Summary Plans of a Region with hourly and monthly prices (USD, excl. taxes)
// @Tags catalog
// @Security BearerAuth
// @Produce json
// @Param region query string true "Region code, e.g. fra1"
// @Success 200 {array} dto.Plan
// @Router /v1/catalog/plans [get]
func (h *Catalog) ListPlans(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req catalog.ListPlansRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	plans, err := h.handler.ListPlans(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, plans)
}

// ListImages godoc
// @Summary Images a Droplet can be installed with
// @Tags catalog
// @Security BearerAuth
// @Produce json
// @Success 200 {array} dto.Image
// @Router /v1/catalog/images [get]
func (h *Catalog) ListImages(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	images, err := h.handler.ListImages(ctx)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, images)
}
