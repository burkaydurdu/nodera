package api

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/constants"
	"github.com/piggystats/nodera/pkg/echo_middleware"
)

const (
	rateLimit  = 50
	HealthPath = "/health"
)

var (
	allowMethods = []string{echo.GET, echo.POST, echo.PUT, echo.PATCH, echo.DELETE, echo.OPTIONS, echo.HEAD}
	allowHeaders = []string{
		echo.HeaderContentType,
		echo.HeaderContentLength,
		echo.HeaderAuthorization,
		echo.HeaderAccept,
		echo.HeaderXRequestedWith,
		echo.HeaderOrigin,
		constants.TargetServerHeader,
	}
	// Retry-After and Content-Disposition are not CORS-safelisted; without
	// exposing them the web app can show neither a lock countdown nor an
	// Invoice PDF's own file name.
	exposeHeaders = []string{echo.HeaderRetryAfter, echo.HeaderContentDisposition, constants.CorrelationIDHeader}
)

// CreateEcho builds the customer gateway's Echo instance.
func CreateEcho(log logger.ILog, allowOrigins []string) *echo.Echo {
	e := newEcho(log, "2MB")
	e.Validator = echo_middleware.NewCustomValidator()

	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins:  allowOrigins,
		AllowMethods:  allowMethods,
		AllowHeaders:  allowHeaders,
		ExposeHeaders: exposeHeaders,
	}))
	e.Use(middleware.RateLimiter(middleware.NewRateLimiterMemoryStore(rateLimit)))

	return e
}

// newEcho is what both gateways share: the platform's error envelope, panic
// recovery, security headers, the body limit and health. Health is answered
// from e.Pre because the proxy chain never calls next.
func newEcho(log logger.ILog, bodyLimit string) *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	e.IPExtractor = echo.ExtractIPFromXFFHeader(echo.TrustLoopback(true), echo.TrustLinkLocal(true), echo.TrustPrivateNet(true))
	e.HTTPErrorHandler = echo_middleware.ErrorHandler(log)

	e.Use(middleware.RecoverWithConfig(middleware.RecoverConfig{StackSize: 1 << 10}))
	e.Use(
		middleware.Secure(),
		middleware.BodyLimit(bodyLimit),
	)

	e.Pre(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if c.Request().Method == http.MethodGet && c.Request().URL.Path == HealthPath {
				return c.JSON(http.StatusOK, "OK")
			}

			return next(c)
		}
	})

	return e
}
