package v1

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/contexts/mail/aggregates/webhook"
	"github.com/piggystats/nodera/contexts/mail/api/v1/middleware"
	"github.com/piggystats/nodera/contexts/mail/application/dto"
)

type IWebhooksHandler interface {
	List(ctx context.Context, req *webhook.ListRequest) ([]*dto.Webhook, error)
	Create(ctx context.Context, req *webhook.CreateRequest) (*dto.Webhook, error)
	Update(ctx context.Context, req *webhook.UpdateRequest) (*dto.Webhook, error)
	Delete(ctx context.Context, req *webhook.GetRequest) error
	Deliveries(ctx context.Context, req *webhook.DeliveriesRequest) (*dto.DeliveryPage, error)
}

type Webhooks struct {
	handler      IWebhooksHandler
	authenticate echo.MiddlewareFunc
}

func NewWebhooks(handler IWebhooksHandler, authenticate echo.MiddlewareFunc) *Webhooks {
	return &Webhooks{handler: handler, authenticate: authenticate}
}

func (h *Webhooks) SetRoutes(group *echo.Group) {
	projects := group.Group("/projects", h.authenticate)
	projects.GET("/:projectID/mail/webhooks", h.List)
	projects.POST("/:projectID/mail/webhooks", h.Create)
	projects.PUT("/:projectID/mail/webhooks/:webhookID", h.Update)
	projects.DELETE("/:projectID/mail/webhooks/:webhookID", h.Delete)
	projects.GET("/:projectID/mail/webhooks/:webhookID/deliveries", h.Deliveries)
}

// List godoc
// @Summary A Project's Webhooks
// @Tags mail
// @Security BearerAuth
// @Produce json
// @Param projectID path string true "Project id"
// @Success 200 {array} dto.Webhook
// @Router /v1/projects/{projectID}/mail/webhooks [get]
func (h *Webhooks) List(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req webhook.ListRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	out, err := h.handler.List(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, out)
}

// Create godoc
// @Summary Add a Webhook
// @Description Answers the signing secret this once. Deliveries are batched JSON arrays signed with Nodera-Signature: t=<unix>,v1=<hex HMAC-SHA256 of "t.body">. At most 3 per Project (409); a public HTTPS URL only (400).
// @Tags mail
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param projectID path string true "Project id"
// @Param request body webhook.CreateRequest true "URL and Message events"
// @Success 201 {object} dto.Webhook
// @Router /v1/projects/{projectID}/mail/webhooks [post]
func (h *Webhooks) Create(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req webhook.CreateRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	out, err := h.handler.Create(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, out)
}

// Update godoc
// @Summary Change a Webhook's URL, events or active flag
// @Tags mail
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param projectID path string true "Project id"
// @Param webhookID path string true "Webhook id"
// @Param request body webhook.UpdateRequest true "URL, events and active"
// @Success 200 {object} dto.Webhook
// @Router /v1/projects/{projectID}/mail/webhooks/{webhookID} [put]
func (h *Webhooks) Update(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req webhook.UpdateRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	out, err := h.handler.Update(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, out)
}

// Delete godoc
// @Summary Delete a Webhook
// @Tags mail
// @Security BearerAuth
// @Param projectID path string true "Project id"
// @Param webhookID path string true "Webhook id"
// @Success 204
// @Router /v1/projects/{projectID}/mail/webhooks/{webhookID} [delete]
func (h *Webhooks) Delete(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req webhook.GetRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	if err := h.handler.Delete(ctx, &req); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}

// Deliveries godoc
// @Summary A Webhook's deliveries of the last 30 days, newest first
// @Tags mail
// @Security BearerAuth
// @Produce json
// @Param projectID path string true "Project id"
// @Param webhookID path string true "Webhook id"
// @Param cursor query string false "next_cursor of the previous page"
// @Param limit query int false "1-100, default 50"
// @Success 200 {object} dto.DeliveryPage
// @Router /v1/projects/{projectID}/mail/webhooks/{webhookID}/deliveries [get]
func (h *Webhooks) Deliveries(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req webhook.DeliveriesRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	out, err := h.handler.Deliveries(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, out)
}
