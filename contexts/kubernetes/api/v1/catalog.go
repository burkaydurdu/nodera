package v1

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/contexts/kubernetes/aggregates/catalog"
	"github.com/piggystats/nodera/contexts/kubernetes/application/dto"
)

type ICatalogHandler interface {
	ListVersions(ctx context.Context) ([]dto.Version, error)
	ListRegions(ctx context.Context) ([]dto.Region, error)
	ListPlans(ctx context.Context, req *catalog.ListPlansRequest) ([]dto.Plan, error)
}

type Catalog struct {
	handler      ICatalogHandler
	authenticate echo.MiddlewareFunc
}

func NewCatalog(handler ICatalogHandler, authenticate echo.MiddlewareFunc) *Catalog {
	return &Catalog{handler: handler, authenticate: authenticate}
}

func (h *Catalog) SetRoutes(group *echo.Group) {
	g := group.Group("/kubernetes", h.authenticate)
	g.GET("/versions", h.ListVersions)
	g.GET("/regions", h.ListRegions)
	g.GET("/regions/:region/plans", h.ListPlans)
}

// ListVersions godoc
// @Summary The Kubernetes versions a Cluster may be opened with, newest first
// @Tags kubernetes
// @Security BearerAuth
// @Produce json
// @Success 200 {array} dto.Version
// @Router /v1/kubernetes/versions [get]
func (h *Catalog) ListVersions(c echo.Context) error {
	out, err := h.handler.ListVersions(c.Request().Context())
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, out)
}

// ListRegions godoc
// @Summary The Regions Kubernetes is sold in
// @Tags kubernetes
// @Security BearerAuth
// @Produce json
// @Success 200 {array} dto.Region
// @Router /v1/kubernetes/regions [get]
func (h *Catalog) ListRegions(c echo.Context) error {
	out, err := h.handler.ListRegions(c.Request().Context())
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, out)
}

// ListPlans godoc
// @Summary The Plans a Node may have in a Region, cheapest first
// @Description Plans with at least 2 CPUs and 4 GB of RAM; sold-out ones stay listed and flagged. 400 for a Region Kubernetes is not sold in.
// @Tags kubernetes
// @Security BearerAuth
// @Produce json
// @Param region path string true "Region code"
// @Success 200 {array} dto.Plan
// @Router /v1/kubernetes/regions/{region}/plans [get]
func (h *Catalog) ListPlans(c echo.Context) error {
	var req catalog.ListPlansRequest
	if err := bind(c, &req); err != nil {
		return err
	}
	out, err := h.handler.ListPlans(c.Request().Context(), &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, out)
}
