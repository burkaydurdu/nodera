package middleware

import (
	"context"
	"errors"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/contexts/customer/aggregates/session"
	"github.com/piggystats/nodera/pkg/constants"
	"github.com/piggystats/nodera/utils/str"
)

// SessionChecker is the slice of the session manager the middleware needs.
type SessionChecker interface {
	Exists(ctx context.Context, sessionID uuid.UUID) (bool, error)
}

// Authenticate verifies the bearer access token and that its session is still
// alive, then stamps the request invariants. The gateway does the same check;
// the service repeats it so a caller reaching it directly gets no shortcut.
func Authenticate(secret string, sessions SessionChecker) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			raw := bearerToken(c)
			if str.IsEmpty(raw) {
				return echo.ErrUnauthorized
			}

			var claims session.AccessClaims
			token, err := jwt.ParseWithClaims(raw, &claims, func(t *jwt.Token) (any, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, errors.New("unexpected signing method")
				}

				return []byte(secret), nil
			})
			if err != nil || !token.Valid || claims.Type != session.TokenTypeAccess {
				return echo.ErrUnauthorized
			}

			sessionID, err := uuid.Parse(claims.SessionID)
			if err != nil {
				return echo.ErrUnauthorized
			}
			if _, err := uuid.Parse(claims.Subject); err != nil {
				return echo.ErrUnauthorized
			}

			alive, err := sessions.Exists(c.Request().Context(), sessionID)
			if err != nil || !alive {
				return echo.ErrUnauthorized
			}

			c.Set(constants.CurrentUserID.String(), claims.Subject)
			c.Set(constants.CurrentUserEmail.String(), claims.Email)
			c.Set(constants.CurrentUserStatus.String(), claims.Status)
			c.Set(constants.CurrentSessionID.String(), claims.SessionID)

			return next(c)
		}
	}
}

func bearerToken(c echo.Context) string {
	const prefix = "Bearer "
	header := c.Request().Header.Get(echo.HeaderAuthorization)
	if len(header) <= len(prefix) || header[:len(prefix)] != prefix {
		return ""
	}

	return header[len(prefix):]
}
