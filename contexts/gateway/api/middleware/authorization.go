package middleware

import (
	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/contexts/gateway/config"
	"github.com/piggystats/nodera/pkg/authz"
	"github.com/piggystats/nodera/pkg/constants"
)

// Authorization resolves an authenticated request against the target
// service's private routes: an unlisted route is a 404, and a route another
// service lists is unlisted here. A route with permissions is then checked
// against the caller's Role in the path's Team (docs/adr/0005): no membership
// reads as a Team that does not exist, a Role without the permission is 403.
// The service repeats the check; the gateway is the first gate, not the only.
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

			if err := permitted(c, endpoint); err != nil {
				return err
			}

			c.Set(constants.TargetHost.String(), service.Host)
			c.Set(constants.DoNotLog.String(), endpoint.DoNotLog)

			return next(c)
		}
	}
}

func permitted(c echo.Context, endpoint *config.Endpoint) error {
	if len(endpoint.Permissions) == 0 {
		return nil
	}

	teamID, ok := endpoint.Param(c.Request().URL.Path, config.TeamIDParam)
	if !ok {
		return echo.ErrNotFound
	}

	teams, _ := c.Get(constants.CurrentUserTeams.String()).(map[string]authz.Role)
	role, member := teams[teamID]
	if !member {
		return echo.ErrNotFound
	}

	for _, permission := range endpoint.Permissions {
		if !authz.Allows(role, authz.Permission(permission)) {
			return echo.ErrForbidden
		}
	}

	return nil
}
