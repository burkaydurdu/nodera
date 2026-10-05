// Package mtahook is the listener only KumoMTA reaches: the signing key for
// a Message and the log records of what happened to it. It is never routed
// through a gateway.
package mtahook

import (
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/contexts/mail/aggregates/sendingdomain"
	"github.com/piggystats/nodera/contexts/mail/application/dto"
	"github.com/piggystats/nodera/utils/hash"
	"github.com/piggystats/nodera/utils/str"
)

// maxRecordsBody bounds one post of log records.
const maxRecordsBody = 16 << 20

type IKeyHandler interface {
	SigningKey(ctx context.Context, req *sendingdomain.KeyRequest) (*dto.DKIMKey, error)
}

type IEventsHandler interface {
	Apply(ctx context.Context, raw []byte) error
}

type MTA struct {
	keys   IKeyHandler
	events IEventsHandler
	token  string
}

func NewMTA(keys IKeyHandler, events IEventsHandler, token string) *MTA {
	return &MTA{keys: keys, events: events, token: token}
}

func (h *MTA) SetRoutes(e *echo.Echo) {
	group := e.Group("/internal/v1", h.authenticate)
	group.GET("/dkim-keys/:domainID", h.SigningKey)
	group.POST("/mta/events", h.Events)
}

// authenticate is a shared bearer token in constant time; KumoMTA holds it.
func (h *MTA) authenticate(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		presented := strings.TrimPrefix(c.Request().Header.Get(echo.HeaderAuthorization), "Bearer ")
		if str.IsEmpty(h.token) || !hash.ConstantTimeEquals(presented, h.token) {
			return echo.ErrUnauthorized
		}

		return next(c)
	}
}

func (h *MTA) SigningKey(c echo.Context) error {
	var req sendingdomain.KeyRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	if err := c.Validate(&req); err != nil {
		return err
	}

	key, err := h.keys.SigningKey(c.Request().Context(), &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, key)
}

// Events answers 2xx once the records are applied; a 5xx makes KumoMTA's log
// hook try the same records again later, which changes nothing applied twice.
func (h *MTA) Events(c echo.Context) error {
	body, err := io.ReadAll(io.LimitReader(c.Request().Body, maxRecordsBody))
	if err != nil {
		return err
	}

	if err := h.events.Apply(c.Request().Context(), body); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}
