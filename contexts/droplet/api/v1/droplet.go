package v1

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/contexts/droplet/aggregates/droplet"
	"github.com/piggystats/nodera/contexts/droplet/api/v1/middleware"
	"github.com/piggystats/nodera/contexts/droplet/application/dto"
)

type IDropletHandler interface {
	List(ctx context.Context, req *droplet.ListRequest) ([]*dto.Droplet, error)
	Create(ctx context.Context, req *droplet.CreateRequest) (*dto.Droplet, error)
	Get(ctx context.Context, req *droplet.GetRequest) (*dto.Droplet, error)
	Action(ctx context.Context, req *droplet.ActionRequest) (*dto.Droplet, error)
	Delete(ctx context.Context, req *droplet.DeleteRequest) (*dto.Droplet, error)
}

type Droplet struct {
	handler      IDropletHandler
	authenticate echo.MiddlewareFunc
}

func NewDroplet(handler IDropletHandler, authenticate echo.MiddlewareFunc) *Droplet {
	return &Droplet{handler: handler, authenticate: authenticate}
}

func (h *Droplet) SetRoutes(group *echo.Group) {
	projects := group.Group("/projects", h.authenticate)
	projects.GET("/:projectID/droplets", h.List)
	projects.POST("/:projectID/droplets", h.Create)

	droplets := group.Group("/droplets", h.authenticate)
	droplets.GET("/:dropletID", h.Get)
	droplets.POST("/:dropletID/actions", h.Action)
	droplets.DELETE("/:dropletID", h.Delete)
}

// List godoc
// @Summary Droplets of a Project the caller's Team owns, newest first
// @Tags droplets
// @Security BearerAuth
// @Produce json
// @Param projectID path string true "Project id"
// @Success 200 {array} dto.Droplet
// @Router /v1/projects/{projectID}/droplets [get]
func (h *Droplet) List(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req droplet.ListRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	droplets, err := h.handler.List(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, droplets)
}

// Create godoc
// @Summary Create a Droplet in a Project
// @Description Answers 201 with the provisioning Droplet; the outcome arrives through list and detail. 409 when the Plan sold out or its price rose since the catalog was loaded.
// @Tags droplets
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param projectID path string true "Project id"
// @Param request body droplet.CreateRequest true "Plan, Region, Image codes, hostname and SSH keys"
// @Success 201 {object} dto.Droplet
// @Router /v1/projects/{projectID}/droplets [post]
func (h *Droplet) Create(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req droplet.CreateRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	created, err := h.handler.Create(ctx, &req)
	if err != nil {
		return withRetryAfter(c, err)
	}

	return c.JSON(http.StatusCreated, created)
}

// Get godoc
// @Summary A Droplet with its status history
// @Tags droplets
// @Security BearerAuth
// @Produce json
// @Param dropletID path string true "Droplet id"
// @Success 200 {object} dto.Droplet
// @Router /v1/droplets/{dropletID} [get]
func (h *Droplet) Get(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req droplet.GetRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	d, err := h.handler.Get(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, d)
}

// Action godoc
// @Summary Power a Droplet on or off, or reboot it
// @Description Allowed from active and off only; the resulting status arrives through detail.
// @Tags droplets
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param dropletID path string true "Droplet id"
// @Param request body droplet.ActionRequest true "power_on | power_off | reboot"
// @Success 202 {object} dto.Droplet
// @Router /v1/droplets/{dropletID}/actions [post]
func (h *Droplet) Action(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req droplet.ActionRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	d, err := h.handler.Action(ctx, &req)
	if err != nil {
		return withRetryAfter(c, err)
	}

	return c.JSON(http.StatusAccepted, d)
}

// Delete godoc
// @Summary Delete a Droplet
// @Description Billing stops at the request; the Droplet answers as deleting until the release is confirmed.
// @Tags droplets
// @Security BearerAuth
// @Produce json
// @Param dropletID path string true "Droplet id"
// @Success 202 {object} dto.Droplet
// @Router /v1/droplets/{dropletID} [delete]
func (h *Droplet) Delete(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req droplet.DeleteRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	d, err := h.handler.Delete(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusAccepted, d)
}
