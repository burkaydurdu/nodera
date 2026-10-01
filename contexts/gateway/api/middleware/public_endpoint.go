package middleware

import (
	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/contexts/gateway/config"
	"github.com/piggystats/nodera/pkg/constants"
)

// PublicSkipper skips the authentication middlewares on a public route.
var PublicSkipper = func(c echo.Context) bool {
	isPublic, ok := c.Get(constants.IsPublic.String()).(bool)

	return ok && isPublic
}

// IsPublicEndpoint marks a route the target service lists as public. Only that
// service's list is consulted: a customer login path sent to the droplet
// target stays private and falls through to the session check.
func IsPublicEndpoint(routeTable *config.GatewayConfig) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			service, ok := targetService(c, routeTable)
			if !ok {
				return echo.ErrUnauthorized
			}

			c.Set(constants.IsPublic.String(), false)

			endpoint := service.PublicEndpoint(c.Request().URL.Path, c.Request().Method)
			if endpoint == nil {
				return next(c)
			}

			c.Set(constants.IsPublic.String(), true)
			c.Set(constants.TargetHost.String(), service.Host)
			c.Set(constants.DoNotLog.String(), endpoint.DoNotLog)

			return next(c)
		}
	}
}
