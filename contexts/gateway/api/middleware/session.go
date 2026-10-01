package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/pkg/authz"
	"github.com/piggystats/nodera/pkg/constants"
	"github.com/piggystats/nodera/utils/str"
)

const (
	tokenTypeAccess  = "access"
	claimType        = "typ"
	claimSessionID   = "sid"
	claimTeams       = "teams"
	claimPermVersion = "pv"
)

// PermVersionKeyFormat is the customer service's permission-version key per
// User (docs/adr/0005); the value is bumped on every membership change and a
// token stamped with an older one is stale.
const PermVersionKeyFormat = "user_perm_version:%s"

type SessionCache interface {
	Exists(ctx context.Context, key string) (bool, error)
	// GetString answers "" for an absent key.
	GetString(ctx context.Context, key string) (string, error)
}

// Session verifies the bearer access token, that the session it names is
// still alive in Redis, so a sign-out is effective on the very next request,
// and that its permission version is current, so a Role change is effective
// just as fast: the 401 sends the web app through its refresh path.
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

			// Tokens issued before docs/adr/0005 carry neither claim; they are
			// stale by definition and refresh once.
			teams, ok := teamsClaim(claims)
			if !ok {
				return echo.ErrUnauthorized
			}
			permVersion, ok := permVersionClaim(claims)
			if !ok {
				return echo.ErrUnauthorized
			}

			ctx := c.Request().Context()
			alive, err := cache.Exists(ctx, sessionKey(sessionKeyFormat, sessionID))
			if err != nil || !alive {
				return echo.ErrUnauthorized
			}

			current, err := currentPermVersion(ctx, cache, userID)
			if err != nil || current != permVersion {
				return echo.ErrUnauthorized
			}

			c.Set(constants.CurrentUserID.String(), userID)
			c.Set(constants.CurrentSessionID.String(), sessionID)
			c.Set(constants.CurrentUserTeams.String(), teams)

			return next(c)
		}
	}
}

// teamsClaim reads the Team id → Role map; an empty object is a User in no
// Team, but a missing or malformed claim is a token the gateway cannot judge.
func teamsClaim(claims jwt.MapClaims) (map[string]authz.Role, bool) {
	raw, ok := claims[claimTeams].(map[string]any)
	if !ok {
		return nil, false
	}

	teams := make(map[string]authz.Role, len(raw))
	for teamID, value := range raw {
		role, ok := value.(string)
		if !ok {
			return nil, false
		}
		teams[teamID] = authz.Role(role)
	}

	return teams, true
}

// permVersionClaim accepts the integer however the JSON decoder delivered it
// and refuses anything that is not a whole number.
func permVersionClaim(claims jwt.MapClaims) (int64, bool) {
	switch value := claims[claimPermVersion].(type) {
	case float64:
		if value != math.Trunc(value) {
			return 0, false
		}

		return int64(value), true
	case json.Number:
		n, err := value.Int64()

		return n, err == nil
	default:
		return 0, false
	}
}

// currentPermVersion treats an absent key as version 0, the value a User who
// has never had a membership change was stamped with.
func currentPermVersion(ctx context.Context, cache SessionCache, userID string) (int64, error) {
	value, err := cache.GetString(ctx, fmt.Sprintf(PermVersionKeyFormat, userID))
	if err != nil {
		return 0, err
	}
	if str.IsEmpty(value) {
		return 0, nil
	}

	return strconv.ParseInt(value, 10, 64)
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
