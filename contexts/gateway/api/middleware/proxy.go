package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/contexts/gateway/config"
	"github.com/piggystats/nodera/pkg/constants"
	"github.com/piggystats/nodera/utils/str"
)

type ReverseProxy interface {
	ServeHTTP(w http.ResponseWriter, r *http.Request)
}

type NewReverseProxy func(target *url.URL) ReverseProxy

var DefaultReverseProxy NewReverseProxy = func(target *url.URL) ReverseProxy {
	return httputil.NewSingleHostReverseProxy(target)
}

var (
	ErrInvalidURL   = errors.New("invalid upstream url")
	ErrNoProxySetup = errors.New("no upstream configured")
)

// Proxies holds one reverse proxy per upstream host.
type Proxies map[string]ReverseProxy

func SetupProxies(gateway *config.GatewayConfig, newProxy NewReverseProxy) (Proxies, error) {
	proxies := make(Proxies)
	for _, service := range gateway.Gateway.Services {
		hostURL, err := url.Parse(service.Host)
		if err != nil {
			return nil, ErrInvalidURL
		}
		proxies[service.Host] = newProxy(hostURL)
	}

	if len(proxies) == 0 {
		return nil, ErrNoProxySetup
	}

	return proxies, nil
}

// TargetHost resolves the target-service header to a service block of the
// route table. The header value is the block's key; a name the table does
// not know is an unlisted target and fails like an unlisted route, before any
// session is looked at. A missing header is a client that does not speak the
// gateway's protocol at all, unless the route is one of the few headerless
// ones whose callers (payment providers, the card issuer) cannot be taught it;
// those name their service by path alone. A header that is present always
// wins, so a headerless route gains nothing from a wrong one.
func TargetHost(routeTable *config.GatewayConfig) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			targetServer := c.Request().Header.Get(constants.TargetServerHeader)
			if str.IsEmpty(targetServer) {
				service, ok := routeTable.HeaderlessService(c.Request().URL.Path, c.Request().Method)
				if !ok {
					return echo.ErrUnauthorized
				}
				targetServer = service
			}

			if _, ok := routeTable.Service(targetServer); !ok {
				return echo.ErrNotFound
			}

			c.Set(constants.TargetServer.String(), targetServer)

			return next(c)
		}
	}
}

// targetService re-reads the service TargetHost resolved. A miss here is
// broken wiring (the middleware ran out of order), not a client mistake.
func targetService(c echo.Context, routeTable *config.GatewayConfig) (config.Service, bool) {
	targetServer, ok := c.Get(constants.TargetServer.String()).(string)
	if !ok {
		return config.Service{}, false
	}

	return routeTable.Service(targetServer)
}

// Proxy forwards the request to its upstream. A proxy that leaves a failure
// through failUpstream has it answered by the platform's error handler, in
// the same shape as every other error.
func Proxy(proxies Proxies) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			host, ok := c.Get(constants.TargetHost.String()).(string)
			if !ok {
				return echo.ErrUnauthorized
			}

			proxy, ok := proxies[host]
			if !ok {
				return echo.ErrUnauthorized
			}

			failure := &upstreamFailure{}
			req := c.Request()
			proxy.ServeHTTP(c.Response(), req.WithContext(context.WithValue(req.Context(), upstreamFailureKey{}, failure)))

			return failure.err
		}
	}
}

type upstreamFailureKey struct{}

// upstreamFailure carries what a proxy's error handler could not forward back
// to Proxy: the handler has a request but no Echo context to return it to.
type upstreamFailure struct {
	err error
}

// failUpstream hands err to the Proxy that forwarded r. A request that came
// through no Proxy has nobody to answer it, so it gets the bare status.
func failUpstream(w http.ResponseWriter, r *http.Request, err *echo.HTTPError) {
	if failure, ok := r.Context().Value(upstreamFailureKey{}).(*upstreamFailure); ok {
		failure.err = err

		return
	}

	w.WriteHeader(err.Code)
}
