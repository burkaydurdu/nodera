package echo_middleware

import (
	"context"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/constants"
	"github.com/piggystats/nodera/utils/hash"
	"github.com/piggystats/nodera/utils/str"
)

// CorrelationID mints a fresh id and publishes it to the outbound request
// header, the Echo context and the response headers. Only the gateway
// registers it; services adopt with GetCorrelationID.
func CorrelationID(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		correlationID := hash.GenerateUUID()
		c.Request().Header.Set(constants.CorrelationIDHeader, correlationID)
		c.Set(constants.CorrelationID.String(), correlationID)
		c.Response().Header().Set(constants.CorrelationIDHeader, correlationID)

		return next(c)
	}
}

// GetCorrelationID adopts the id a gateway minted, but only when it parses as
// a UUID: a caller reaching the service directly must not be able to write
// arbitrary bytes into every log line the request produces.
func GetCorrelationID(log logger.ILog) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			correlationID := c.Request().Header.Get(constants.CorrelationIDHeader)
			if str.IsNotEmpty(correlationID) {
				if _, err := uuid.Parse(correlationID); err != nil {
					log.WarnWithFields("inbound correlation header rejected: not a UUID", logger.NewFields().
						Add("path", c.Path()).
						Add("method", c.Request().Method))
					correlationID = ""
				}
			}

			if str.IsEmpty(correlationID) {
				correlationID = hash.GenerateUUID()
			}

			c.Set(constants.CorrelationID.String(), correlationID)
			injectIntoRequestContext(c, correlationID)

			return next(c)
		}
	}
}

// ClientInfo copies the caller's address and user agent onto the request
// context so a handler that opens a session can describe the device.
func ClientInfo(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		req := c.Request()
		ctx := context.WithValue(req.Context(), constants.ClientIP, c.RealIP())
		ctx = context.WithValue(ctx, constants.ClientUserAgent, req.UserAgent())
		c.SetRequest(req.WithContext(ctx))

		return next(c)
	}
}

func injectIntoRequestContext(c echo.Context, correlationID string) {
	req := c.Request()
	ctx := context.WithValue(req.Context(), constants.CorrelationID, correlationID)
	c.SetRequest(req.WithContext(ctx))
}
