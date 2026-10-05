package v1

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/contexts/mail/aggregates/sendingdomain"
	"github.com/piggystats/nodera/contexts/mail/api"
	"github.com/piggystats/nodera/contexts/mail/api/v1/middleware"
	"github.com/piggystats/nodera/contexts/mail/application/dto"
)

type ISendingDomainsHandler interface {
	List(ctx context.Context, req *sendingdomain.ListRequest) ([]*dto.SendingDomain, error)
	Add(ctx context.Context, req *sendingdomain.AddRequest) (*dto.SendingDomain, error)
	Get(ctx context.Context, req *sendingdomain.GetRequest) (*dto.SendingDomain, error)
	Delete(ctx context.Context, req *sendingdomain.GetRequest) error
	Verify(ctx context.Context, req *sendingdomain.GetRequest) (*dto.SendingDomain, error)
}

type SendingDomains struct {
	handler      ISendingDomainsHandler
	authenticate echo.MiddlewareFunc
}

func NewSendingDomains(handler ISendingDomainsHandler, authenticate echo.MiddlewareFunc) *SendingDomains {
	return &SendingDomains{handler: handler, authenticate: authenticate}
}

func (h *SendingDomains) SetRoutes(group *echo.Group) {
	projects := group.Group("/projects", h.authenticate)
	projects.GET("/:projectID/mail/domains", h.List)
	projects.POST("/:projectID/mail/domains", h.Add)
	projects.GET("/:projectID/mail/domains/:domainID", h.Get)
	projects.DELETE("/:projectID/mail/domains/:domainID", h.Delete)
	projects.POST("/:projectID/mail/domains/:domainID/verify", h.Verify)
}

// List godoc
// @Summary A Project's Sending domains
// @Tags mail
// @Security BearerAuth
// @Produce json
// @Param projectID path string true "Project id"
// @Success 200 {array} dto.SendingDomain
// @Router /v1/projects/{projectID}/mail/domains [get]
func (h *SendingDomains) List(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req sendingdomain.ListRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	out, err := h.handler.List(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, out)
}

// Add godoc
// @Summary Add a Sending domain
// @Description Answers the three CNAME records to add. 409 when the Project already has the domain or holds 10. 403 without mail.write.
// @Tags mail
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param projectID path string true "Project id"
// @Param request body sendingdomain.AddRequest true "The domain"
// @Success 201 {object} dto.SendingDomain
// @Router /v1/projects/{projectID}/mail/domains [post]
func (h *SendingDomains) Add(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req sendingdomain.AddRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	out, err := h.handler.Add(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, out)
}

// Get godoc
// @Summary A Sending domain with its records and what the last check found
// @Tags mail
// @Security BearerAuth
// @Produce json
// @Param projectID path string true "Project id"
// @Param domainID path string true "Sending domain id"
// @Success 200 {object} dto.SendingDomain
// @Router /v1/projects/{projectID}/mail/domains/{domainID} [get]
func (h *SendingDomains) Get(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req sendingdomain.GetRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	out, err := h.handler.Get(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, out)
}

// Delete godoc
// @Summary Delete a Sending domain
// @Description Sending from it stops at once; its Messages' logs stay until they expire.
// @Tags mail
// @Security BearerAuth
// @Param projectID path string true "Project id"
// @Param domainID path string true "Sending domain id"
// @Success 204
// @Router /v1/projects/{projectID}/mail/domains/{domainID} [delete]
func (h *SendingDomains) Delete(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req sendingdomain.GetRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	if err := h.handler.Delete(ctx, &req); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}

// Verify godoc
// @Summary Check a Sending domain's records now
// @Description At most once a minute per domain (429 with Retry-After).
// @Tags mail
// @Security BearerAuth
// @Produce json
// @Param projectID path string true "Project id"
// @Param domainID path string true "Sending domain id"
// @Success 200 {object} dto.SendingDomain
// @Router /v1/projects/{projectID}/mail/domains/{domainID}/verify [post]
func (h *SendingDomains) Verify(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req sendingdomain.GetRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	out, err := h.handler.Verify(ctx, &req)
	if err != nil {
		return api.WithRetryAfter(c, err)
	}

	return c.JSON(http.StatusOK, out)
}
