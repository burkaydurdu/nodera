package middleware

import (
	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/contexts/gateway/config"
	"github.com/piggystats/nodera/pkg/constants"
)

// Authorization resolves an authenticated request against the target
// service's private routes: an unlisted route is a 404, and a route another
// service lists is unlisted here. Permissions are declared per endpoint but
// not yet enforced in this phase, because no token carries any.
func Authorization(routeTable *config.GatewayConfig, skipper func(echo.Context) bool) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if skipper(c) {
				return next(c)
			}

			service, ok := targetService(c, routeTable)
			if !ok {
				return echo.ErrUnauthorized
			}

			endpoint := service.PrivateEndpoint(c.Request().URL.Path, c.Request().Method)
			if endpoint == nil {
				return echo.ErrNotFound
			}

			if len(endpoint.Permissions) > 0 {
				return echo.ErrForbidden
			}

			c.Set(constants.TargetHost.String(), service.Host)
			c.Set(constants.DoNotLog.String(), endpoint.DoNotLog)

			return next(c)
		}
	}
}
