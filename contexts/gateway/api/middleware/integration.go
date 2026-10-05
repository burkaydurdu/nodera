package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/contexts/gateway/config"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/apperror"
	"github.com/piggystats/nodera/pkg/constants"
)

const (
	apiKeyRateLimit       = 100
	apiKeyRateLimitWindow = time.Second
)

const integrationRoute constants.ContextKey = "integrationRoute"

type APIKeyRateLimiter interface {
	SlidingWindowAllow(ctx context.Context, key string, window time.Duration, limit int64) (bool, error)
}

type APIKeyUseRecorder interface {
	Set(ctx context.Context, key string, value any, expiration time.Duration) error
}

// IntegrationCache is everything the integration gateway asks Redis: the
// keys, their budgets and their last use.
type IntegrationCache interface {
	APIKeyReader
	APIKeyRateLimiter
	APIKeyUseRecorder
}

// ResolveIntegrationRoute finds the request's route by method and path alone:
// software sends no target header. An unlisted route is a 404 before any key
// is read.
func ResolveIntegrationRoute(table *config.IntegrationGatewayConfig) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			route, ok := table.Route(c.Request().URL.Path, c.Request().Method)
			if !ok {
				return echo.ErrNotFound
			}

			c.Set(integrationRoute.String(), route)
			c.Set(constants.TargetHost.String(), route.Host)

			return next(c)
		}
	}
}

// APIKeyAuthorization refuses a key that lacks any Permission the route
// lists. The key already names its one Project, so there is no membership to
// look up: a missing Permission is a 403. The service repeats the check; the
// gateway is the first gate, not the only one.
func APIKeyAuthorization() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			route, ok := c.Get(integrationRoute.String()).(config.Route)
			if !ok {
				return echo.ErrUnauthorized
			}
			key, ok := currentAPIKey(c)
			if !ok {
				return echo.ErrUnauthorized
			}

			for _, permission := range route.Endpoint.Permissions {
				if !slices.Contains(key.permissions, permission) {
					return echo.ErrForbidden
				}
			}

			return next(c)
		}
	}
}

// APIKeyRateLimit holds each key to 100 requests a second across every
// replica. A refused request still counts, so a caller that ignores
// Retry-After stays refused. Redis failing here lets the request through,
// like every limiter of the platform: the key itself was just read, and the
// mail service keeps its own limits.
func APIKeyRateLimit(limiter APIKeyRateLimiter, log logger.ILog) echo.MiddlewareFunc {
	retryAfter := strconv.Itoa(int(apiKeyRateLimitWindow.Seconds()))

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			key, ok := currentAPIKey(c)
			if !ok {
				return echo.ErrUnauthorized
			}

			allowed, err := limiter.SlidingWindowAllow(c.Request().Context(), fmt.Sprintf(APIKeyRateLimitKeyFormat, key.id), apiKeyRateLimitWindow, apiKeyRateLimit)
			if err != nil {
				log.WarnWithFields("api key rate limit unavailable, request let through", logger.NewFields().
					Add("api_key_id", key.id).
					Add("error", err.Error()))

				return next(c)
			}
			if !allowed {
				c.Response().Header().Set(echo.HeaderRetryAfter, retryAfter)

				return apperror.ErrorRateLimited.Copy()
			}

			return next(c)
		}
	}
}

// RecordAPIKeyUse stamps the key's last use before the request goes on. It
// is bookkeeping for the key list, so failing to write it never fails a
// send.
func RecordAPIKeyUse(recorder APIKeyUseRecorder, log logger.ILog) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			key, ok := currentAPIKey(c)
			if !ok {
				return echo.ErrUnauthorized
			}

			usedAt := time.Now().UTC().Format(time.RFC3339Nano)
			if err := recorder.Set(c.Request().Context(), fmt.Sprintf(APIKeyLastUsedKeyFormat, key.id), usedAt, 0); err != nil {
				log.WarnWithFields("api key last use not recorded", logger.NewFields().
					Add("api_key_id", key.id).
					Add("error", err.Error()))
			}

			return next(c)
		}
	}
}

func SetupIntegrationProxies(table *config.IntegrationGatewayConfig, newProxy NewReverseProxy) (Proxies, error) {
	return SetupProxies(&config.GatewayConfig{Gateway: table.Gateway}, newProxy)
}

// NewIntegrationReverseProxy is the integration gateway's proxy. A body that
// declares its length is held to the body limit before anything is read; a
// chunked one is cut short while it streams upstream, and that is answered
// as the same 413 rather than the proxy's bare 502. Every other upstream
// failure is a 502, logged with its cause. Both go back through Proxy, so
// they reach the caller in the platform's error shape.
func NewIntegrationReverseProxy(log logger.ILog) NewReverseProxy {
	return func(target *url.URL) ReverseProxy {
		proxy := httputil.NewSingleHostReverseProxy(target)
		proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			if errors.Is(err, echo.ErrStatusRequestEntityTooLarge) {
				failUpstream(w, r, echo.ErrStatusRequestEntityTooLarge)

				return
			}

			log.ErrorWithFields(err, logger.NewFields().
				Add("upstream", target.String()).
				Add("method", r.Method).
				Add("path", r.URL.Path))
			failUpstream(w, r, echo.ErrBadGateway)
		}

		return proxy
	}
}
