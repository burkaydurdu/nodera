package v1

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/contexts/storage/aggregates/usage"
	"github.com/piggystats/nodera/contexts/storage/api/v1/middleware"
	"github.com/piggystats/nodera/contexts/storage/application/dto"
)

type IUsageHandler interface {
	Show(ctx context.Context, req *usage.Request) (*dto.Usage, error)
}

type Usage struct {
	handler      IUsageHandler
	authenticate echo.MiddlewareFunc
}

func NewUsage(handler IUsageHandler, authenticate echo.MiddlewareFunc) *Usage {
	return &Usage{handler: handler, authenticate: authenticate}
}

func (h *Usage) SetRoutes(group *echo.Group) {
	projects := group.Group("/projects", h.authenticate)
	projects.GET("/:projectID/storages/:storageID/usage", h.Show)
}

// Show godoc
// @Summary A Storage's size and traffic per UTC day, the last 30 days
// @Tags storage
// @Security BearerAuth
// @Produce json
// @Param projectID path string true "Project id"
// @Param storageID path string true "Storage id"
// @Success 200 {object} dto.Usage
// @Router /v1/projects/{projectID}/storages/{storageID}/usage [get]
func (h *Usage) Show(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}
	var req usage.Request
	if err := bind(c, &req); err != nil {
		return err
	}
	out, err := h.handler.Show(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, out)
}
