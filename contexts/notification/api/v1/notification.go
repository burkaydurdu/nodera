package v1

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/contexts/notification/aggregates/notification"
	"github.com/piggystats/nodera/contexts/notification/api/v1/middleware"
	"github.com/piggystats/nodera/contexts/notification/application/dto"
)

type IInboxHandler interface {
	List(ctx context.Context, req *notification.ListRequest) ([]*dto.Notification, error)
	UnreadCount(ctx context.Context) (*dto.UnreadCount, error)
	Read(ctx context.Context, req *notification.ReadRequest) (*dto.Notification, error)
	ReadAll(ctx context.Context) (*dto.ReadAll, error)
}

type Inbox struct {
	handler      IInboxHandler
	authenticate echo.MiddlewareFunc
}

func NewInbox(handler IInboxHandler, authenticate echo.MiddlewareFunc) *Inbox {
	return &Inbox{handler: handler, authenticate: authenticate}
}

func (h *Inbox) SetRoutes(group *echo.Group) {
	notifications := group.Group("/notifications", h.authenticate)
	notifications.GET("", h.List)
	notifications.GET("/unread-count", h.UnreadCount)
	notifications.PUT("/read-all", h.ReadAll)
	notifications.PUT("/:notificationID/read", h.Read)
}

// List godoc
// @Summary The caller's Inbox across every Team they are in, newest first
// @Description A Team's Notifications show only while the caller is a member. Page with before = the last id of the previous page; a page shorter than limit is the last.
// @Tags notifications
// @Security BearerAuth
// @Produce json
// @Param before query string false "Id of the last Notification of the previous page"
// @Param limit query int false "Page size, 1 to 50, default 20"
// @Success 200 {array} dto.Notification
// @Router /v1/notifications [get]
func (h *Inbox) List(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req notification.ListRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	items, err := h.handler.List(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, items)
}

// UnreadCount godoc
// @Summary How many Notifications in the caller's Inbox are unread
// @Tags notifications
// @Security BearerAuth
// @Produce json
// @Success 200 {object} dto.UnreadCount
// @Router /v1/notifications/unread-count [get]
func (h *Inbox) UnreadCount(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	count, err := h.handler.UnreadCount(ctx)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, count)
}

// Read godoc
// @Summary Mark one Notification read
// @Description Reading it again keeps the first time. 404 for a Notification that is not the caller's or no longer visible to them. The caller's other tabs learn it live.
// @Tags notifications
// @Security BearerAuth
// @Produce json
// @Param notificationID path string true "Notification id"
// @Success 200 {object} dto.Notification
// @Router /v1/notifications/{notificationID}/read [put]
func (h *Inbox) Read(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req notification.ReadRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	n, err := h.handler.Read(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, n)
}

// ReadAll godoc
// @Summary Mark every Notification in the caller's Inbox read
// @Tags notifications
// @Security BearerAuth
// @Produce json
// @Success 200 {object} dto.ReadAll
// @Router /v1/notifications/read-all [put]
func (h *Inbox) ReadAll(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	count, err := h.handler.ReadAll(ctx)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, count)
}
