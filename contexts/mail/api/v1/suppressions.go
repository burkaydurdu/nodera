package v1

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/contexts/mail/aggregates/suppression"
	"github.com/piggystats/nodera/contexts/mail/api/v1/middleware"
	"github.com/piggystats/nodera/contexts/mail/application/dto"
)

type ISuppressionsHandler interface {
	List(ctx context.Context, req *suppression.ListRequest) (*dto.SuppressionPage, error)
	Delete(ctx context.Context, req *suppression.DeleteRequest) error
}

type Suppressions struct {
	handler      ISuppressionsHandler
	authenticate echo.MiddlewareFunc
}

func NewSuppressions(handler ISuppressionsHandler, authenticate echo.MiddlewareFunc) *Suppressions {
	return &Suppressions{handler: handler, authenticate: authenticate}
}

func (h *Suppressions) SetRoutes(group *echo.Group) {
	projects := group.Group("/projects", h.authenticate)
	projects.GET("/:projectID/mail/suppressions", h.List)
	projects.DELETE("/:projectID/mail/suppressions/:suppressionID", h.Delete)
}

// List godoc
// @Summary A Project's Suppressions, newest first
// @Tags mail
// @Security BearerAuth
// @Produce json
// @Param projectID path string true "Project id"
// @Param search query string false "Part of an address"
// @Param cursor query string false "next_cursor of the previous page"
// @Param limit query int false "1-100, default 50"
// @Success 200 {object} dto.SuppressionPage
// @Router /v1/projects/{projectID}/mail/suppressions [get]
func (h *Suppressions) List(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req suppression.ListRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	out, err := h.handler.List(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, out)
}

// Delete godoc
// @Summary Mail an address again
// @Description Only a bounce Suppression can be removed; a complaint answers 409.
// @Tags mail
// @Security BearerAuth
// @Param projectID path string true "Project id"
// @Param suppressionID path string true "Suppression id"
// @Success 204
// @Router /v1/projects/{projectID}/mail/suppressions/{suppressionID} [delete]
func (h *Suppressions) Delete(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req suppression.DeleteRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	if err := h.handler.Delete(ctx, &req); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}
