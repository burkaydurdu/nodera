package v1

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/contexts/mail/aggregates/packages"
	"github.com/piggystats/nodera/contexts/mail/api/v1/middleware"
	"github.com/piggystats/nodera/contexts/mail/application/dto"
)

type IPackagesHandler interface {
	Catalog(ctx context.Context) ([]dto.Package, error)
	Overview(ctx context.Context, req *packages.OverviewRequest) (*dto.Overview, error)
	Choose(ctx context.Context, req *packages.ChooseRequest) (*dto.Overview, error)
}

type Packages struct {
	handler      IPackagesHandler
	authenticate echo.MiddlewareFunc
}

func NewPackages(handler IPackagesHandler, authenticate echo.MiddlewareFunc) *Packages {
	return &Packages{handler: handler, authenticate: authenticate}
}

func (h *Packages) SetRoutes(group *echo.Group) {
	group.GET("/mail/packages", h.Catalog, h.authenticate)

	projects := group.Group("/projects", h.authenticate)
	projects.GET("/:projectID/mail", h.Overview)
	projects.PUT("/:projectID/mail/package", h.Choose)
}

// Catalog godoc
// @Summary The Packages a Project can choose, smallest first
// @Description Prices are USD per month, excluding taxes.
// @Tags mail
// @Security BearerAuth
// @Produce json
// @Success 200 {array} dto.Package
// @Router /v1/mail/packages [get]
func (h *Packages) Catalog(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	list, err := h.handler.Catalog(ctx)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, list)
}

// Overview godoc
// @Summary A Project's Mail at a glance
// @Description The Package and any scheduled change, this Period's usage, today's Daily limit, the seven-day rates, a Pause and the Team's Billing status. sending_state says whether a Message would be accepted now. 403 without mail.read; 404 for a Project the caller is not a member of.
// @Tags mail
// @Security BearerAuth
// @Produce json
// @Param projectID path string true "Project id"
// @Success 200 {object} dto.Overview
// @Router /v1/projects/{projectID}/mail [get]
func (h *Packages) Overview(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req packages.OverviewRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	out, err := h.handler.Overview(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, out)
}

// Choose godoc
// @Summary Choose, change or cancel a Project's Package
// @Description A first Package and a larger one apply at once (the used count carries over); a smaller one and "none" apply from the next Period, and choosing the current Package again undoes them. Starting or upgrading needs a payment method (402) and a Team that is not suspended (423). 403 without mail.write.
// @Tags mail
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param projectID path string true "Project id"
// @Param request body packages.ChooseRequest true "A Package code or none"
// @Success 200 {object} dto.Overview
// @Router /v1/projects/{projectID}/mail/package [put]
func (h *Packages) Choose(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req packages.ChooseRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	out, err := h.handler.Choose(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, out)
}
