package v1

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/contexts/mail/aggregates/message"
	"github.com/piggystats/nodera/contexts/mail/api/v1/middleware"
	"github.com/piggystats/nodera/contexts/mail/application/dto"
)

type ILogsHandler interface {
	List(ctx context.Context, req *message.ListRequest) (*dto.MessagePage, error)
	Get(ctx context.Context, req *message.GetRequest) (*dto.MessageDetail, error)
}

type Messages struct {
	handler      ILogsHandler
	authenticate echo.MiddlewareFunc
}

func NewMessages(handler ILogsHandler, authenticate echo.MiddlewareFunc) *Messages {
	return &Messages{handler: handler, authenticate: authenticate}
}

func (h *Messages) SetRoutes(group *echo.Group) {
	projects := group.Group("/projects", h.authenticate)
	projects.GET("/:projectID/mail/messages", h.List)
	projects.GET("/:projectID/mail/messages/:messageID", h.Get)
}

// List godoc
// @Summary A Project's Messages of the last 30 days, newest first
// @Description A status or a recipient filter matches a Message when one of its recipients does. Nodera keeps no Message body.
// @Tags mail
// @Security BearerAuth
// @Produce json
// @Param projectID path string true "Project id"
// @Param status query string false "accepted | delivered | deferred | bounced | suppressed | complained"
// @Param recipient query string false "Recipient address"
// @Param from query string false "Sender address"
// @Param tag query string false "Tag"
// @Param since query string false "RFC 3339"
// @Param until query string false "RFC 3339"
// @Param cursor query string false "next_cursor of the previous page"
// @Param limit query int false "1-100, default 50"
// @Success 200 {object} dto.MessagePage
// @Router /v1/projects/{projectID}/mail/messages [get]
func (h *Messages) List(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req message.ListRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	out, err := h.handler.List(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, out)
}

// Get godoc
// @Summary A Message with each recipient's Message events
// @Tags mail
// @Security BearerAuth
// @Produce json
// @Param projectID path string true "Project id"
// @Param messageID path string true "Message id"
// @Success 200 {object} dto.MessageDetail
// @Router /v1/projects/{projectID}/mail/messages/{messageID} [get]
func (h *Messages) Get(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req message.GetRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	out, err := h.handler.Get(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, out)
}
