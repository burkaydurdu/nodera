package v1

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/contexts/storage/api/v1/middleware"
	"github.com/piggystats/nodera/contexts/storage/application/dto"
)

type ICatalogHandler interface {
	Regions(ctx context.Context) ([]dto.Region, error)
	Packages(ctx context.Context) ([]dto.Package, error)
}

type Catalog struct {
	handler      ICatalogHandler
	authenticate echo.MiddlewareFunc
}

func NewCatalog(handler ICatalogHandler, authenticate echo.MiddlewareFunc) *Catalog {
	return &Catalog{handler: handler, authenticate: authenticate}
}

func (h *Catalog) SetRoutes(group *echo.Group) {
	group.GET("/storage/regions", h.Regions, h.authenticate)
	group.GET("/storage/packages", h.Packages, h.authenticate)
}

// Regions godoc
// @Summary The Regions a Storage can be opened in
// @Tags storage
// @Security BearerAuth
// @Produce json
// @Success 200 {array} dto.Region
// @Router /v1/storage/regions [get]
func (h *Catalog) Regions(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}
	list, err := h.handler.Regions(ctx)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, list)
}

// Packages godoc
// @Summary The Packages a Storage can be on, smallest first
// @Description Sizes are bytes (1 GB is 1,000,000,000 bytes); prices are USD per month, excluding taxes.
// @Tags storage
// @Security BearerAuth
// @Produce json
// @Success 200 {array} dto.Package
// @Router /v1/storage/packages [get]
func (h *Catalog) Packages(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}
	list, err := h.handler.Packages(ctx)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, list)
}
