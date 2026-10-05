// Package send is the sending API the integration gateway fronts
// (docs/adr/0010): software holding an API key sends a Message.
package send

import (
	"context"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/contexts/mail/aggregates/message"
	"github.com/piggystats/nodera/contexts/mail/api"
	"github.com/piggystats/nodera/contexts/mail/apikey"
	"github.com/piggystats/nodera/contexts/mail/application/dto"
	"github.com/piggystats/nodera/pkg/authz"
	"github.com/piggystats/nodera/pkg/constants"
)

const keyContextKey = "mail_api_key"

type ISendHandler interface {
	SendHTTP(ctx context.Context, key *apikey.Key, req *message.SendRequest) (*dto.Sent, error)
}

type KeyVerifier interface {
	Verify(ctx context.Context, raw string, permission authz.Permission) (*apikey.Key, error)
}

type Messages struct {
	handler  ISendHandler
	verifier KeyVerifier
}

func NewMessages(handler ISendHandler, verifier KeyVerifier) *Messages {
	return &Messages{handler: handler, verifier: verifier}
}

func (h *Messages) SetRoutes(group *echo.Group) {
	group.POST("/mail/messages", h.Send, h.authenticate)
}

// authenticate checks the key again behind the gateway: the gateway is the
// first gate, never the only one.
func (h *Messages) authenticate(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		raw := strings.TrimPrefix(c.Request().Header.Get(echo.HeaderAuthorization), "Bearer ")
		key, err := h.verifier.Verify(c.Request().Context(), raw, authz.MailSend)
		if err != nil {
			return err
		}
		c.Set(keyContextKey, key)

		return next(c)
	}
}

// Send godoc
// @Summary Send a Message
// @Description At most 50 recipients and 10 MB decoded. 202 with the Message id once accepted; suppressed recipients are dropped and named. 402 without a Package, 429 with Retry-After when the Daily limit or the Package is used up, 423 when the Project is Paused or the Team suspended, 422 when the sender is not on a verified Sending domain. A repeated Idempotency-Key within 24 hours answers the first Message.
// @Tags sending
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param Idempotency-Key header string false "Repeat-safe request id"
// @Param request body message.SendRequest true "The Message"
// @Success 202 {object} dto.Sent
// @Router /v1/mail/messages [post]
func (h *Messages) Send(c echo.Context) error {
	key, _ := c.Get(keyContextKey).(*apikey.Key)

	var req message.SendRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	if err := (&echo.DefaultBinder{}).BindHeaders(c, &req); err != nil {
		return err
	}
	if err := c.Validate(&req); err != nil {
		return err
	}

	ctx := c.Request().Context()
	if correlationID, ok := c.Get(constants.CorrelationID.String()).(string); ok {
		ctx = context.WithValue(ctx, constants.CorrelationID, correlationID)
	}

	sent, err := h.handler.SendHTTP(ctx, key, &req)
	if err != nil {
		return api.WithRetryAfter(c, err)
	}

	return c.JSON(http.StatusAccepted, sent)
}
