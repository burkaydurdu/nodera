package middleware

import (
	"context"
	"errors"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/pkg/constants"
	"github.com/piggystats/nodera/utils/str"
)

const (
	tokenTypeAccess = "access"
	claimType       = "typ"
	claimSessionID  = "sid"
)

type SessionCache interface {
	Exists(ctx context.Context, key string) (bool, error)
}

// Session verifies the bearer access token and that the session it names is
// still alive in Redis, so a sign-out is effective on the very next request.
func Session(secret string, cache SessionCache, sessionKeyFormat string, skipper func(echo.Context) bool) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if skipper(c) {
				return next(c)
			}

			raw := bearerToken(c)
			if str.IsEmpty(raw) {
				return echo.ErrUnauthorized
			}

			claims := jwt.MapClaims{}
			token, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, errors.New("unexpected signing method")
				}

				return []byte(secret), nil
			})
			if err != nil || !token.Valid {
				return echo.ErrUnauthorized
			}

			if typ, _ := claims[claimType].(string); typ != tokenTypeAccess {
				return echo.ErrUnauthorized
			}

			sessionID, _ := claims[claimSessionID].(string)
			if _, err := uuid.Parse(sessionID); err != nil {
				return echo.ErrUnauthorized
			}

			userID, err := claims.GetSubject()
			if err != nil || str.IsEmpty(userID) {
				return echo.ErrUnauthorized
			}

			alive, err := cache.Exists(c.Request().Context(), sessionKey(sessionKeyFormat, sessionID))
			if err != nil || !alive {
				return echo.ErrUnauthorized
			}

			c.Set(constants.CurrentUserID.String(), userID)
			c.Set(constants.CurrentSessionID.String(), sessionID)

			return next(c)
		}
	}
}

func sessionKey(format, sessionID string) string {
	return fmt.Sprintf(format, sessionID)
}

func bearerToken(c echo.Context) string {
	const prefix = "Bearer "
	header := c.Request().Header.Get(echo.HeaderAuthorization)
	if len(header) <= len(prefix) || header[:len(prefix)] != prefix {
		return ""
	}

	return header[len(prefix):]
}
