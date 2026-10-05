package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/pkg/apperror"
	"github.com/piggystats/nodera/pkg/constants"
	"github.com/piggystats/nodera/utils/hash"
	"github.com/piggystats/nodera/utils/str"
)

// Redis keys of an API key (docs/adr/0010). gosec's G101 matches "APIKey" in
// a constant's name; these are Redis key names.
const (
	// APIKeyKeyFormat is the customer context's record of the key, written
	// when the key is created and deleted when it is revoked: a missing
	// record is a key that never existed or no longer does, and a revocation
	// holds from the very next request.
	APIKeyKeyFormat = "api_key:%s" // #nosec G101 - a Redis key name
	// APIKeyLastUsedKeyFormat holds when the key last passed the gateway, as
	// RFC 3339 with nanoseconds in UTC; the customer service shows it beside
	// the key so a Team can find the ones nothing uses.
	APIKeyLastUsedKeyFormat = "api_key_last_used:%s" // #nosec G101 - a Redis key name
	// APIKeyRateLimitKeyFormat is the key's sliding window.
	APIKeyRateLimitKeyFormat = "ratelimit:integration:%s" // #nosec G101 - a Redis key name
)

const (
	apiKeyScheme = "Bearer "
	apiKeyPrefix = "nd_"
	apiKeyIDSize = 12
	// apiKeyMaxSecretSize is the longest base62 rendering of the 32 random
	// bytes behind a secret; a rendering that drops leading zeros is shorter.
	apiKeyMaxSecretSize = 43
)

const authenticatedAPIKey constants.ContextKey = "authenticatedAPIKey"

// apiKeyRecord is the part of the record the gateway reads; the Project and
// Team it also names are the mail service's business.
type apiKeyRecord struct {
	Hash        string   `json:"hash"`
	Permissions []string `json:"permissions"`
}

// authenticatedKey is what the rest of the chain needs of a key that passed:
// its id for its budget and last use, its Permissions for the route.
type authenticatedKey struct {
	id          string
	permissions []string
}

type APIKeyReader interface {
	// GetString answers "" for an absent key.
	GetString(ctx context.Context, key string) (string, error)
}

// APIKey authenticates the caller by the API key in the Authorization
// header. A missing, malformed, unknown, revoked or wrong key is the same
// 401, so a caller learns nothing about which part failed. A record that
// cannot be read, Redis being unreachable or the entry undecodable, is not
// the caller's fault and answers 500, which software retries. Session may
// answer an outage with a 401, which only sends the web app through its
// refresh; here a 401 would tell software to give up on a good key.
func APIKey(reader APIKeyReader) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			id, secret, ok := parseAPIKey(c.Request().Header.Get(echo.HeaderAuthorization))
			if !ok {
				return echo.ErrUnauthorized
			}

			raw, err := reader.GetString(c.Request().Context(), fmt.Sprintf(APIKeyKeyFormat, id))
			if err != nil {
				return apperror.ErrorCommon.Copy().SetError(err).Add("api_key_id", id)
			}
			if str.IsEmpty(raw) {
				return echo.ErrUnauthorized
			}

			var record apiKeyRecord
			if err := json.Unmarshal([]byte(raw), &record); err != nil {
				return apperror.ErrorCommon.Copy().SetError(err).Add("api_key_id", id)
			}

			if !hash.ConstantTimeEquals(hash.SHA256Hex(secret), record.Hash) {
				return echo.ErrUnauthorized
			}

			c.Set(authenticatedAPIKey.String(), authenticatedKey{id: id, permissions: record.Permissions})

			return next(c)
		}
	}
}

// parseAPIKey accepts exactly "Bearer nd_<id>.<secret>": an id of 12 and a
// secret of at most 43 base62 characters. Anything looser could not have
// been issued, so it is refused before Redis is asked.
func parseAPIKey(header string) (id, secret string, ok bool) {
	key, found := strings.CutPrefix(header, apiKeyScheme)
	if !found {
		return "", "", false
	}

	key, found = strings.CutPrefix(key, apiKeyPrefix)
	if !found {
		return "", "", false
	}

	id, secret, found = strings.Cut(key, ".")
	if !found || len(id) != apiKeyIDSize || !isBase62(id) {
		return "", "", false
	}
	if str.IsEmpty(secret) || len(secret) > apiKeyMaxSecretSize || !isBase62(secret) {
		return "", "", false
	}

	return id, secret, true
}

func isBase62(s string) bool {
	for i := 0; i < len(s); i++ {
		if !isBase62Digit(s[i]) {
			return false
		}
	}

	return true
}

func isBase62Digit(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// currentAPIKey re-reads the key APIKey authenticated. A miss here is broken
// wiring (the middleware ran out of order), not a client mistake, and fails
// closed.
func currentAPIKey(c echo.Context) (authenticatedKey, bool) {
	key, ok := c.Get(authenticatedAPIKey.String()).(authenticatedKey)

	return key, ok
}
