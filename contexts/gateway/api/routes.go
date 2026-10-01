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
	// Retry-After is not CORS-safelisted; without exposing it the web app
	// cannot show a lock countdown.
	exposeHeaders = []string{echo.HeaderRetryAfter, constants.CorrelationIDHeader}
)

// CreateEcho builds the gateway's Echo instance. Health is answered from
// e.Pre because the proxy chain below never calls next.
func CreateEcho(log logger.ILog, allowOrigins []string) *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	e.IPExtractor = echo.ExtractIPFromXFFHeader(echo.TrustLoopback(true), echo.TrustLinkLocal(true), echo.TrustPrivateNet(true))
	e.Validator = echo_middleware.NewCustomValidator()
	e.HTTPErrorHandler = echo_middleware.ErrorHandler(log)

	e.Use(middleware.RecoverWithConfig(middleware.RecoverConfig{StackSize: 1 << 10}))
	e.Use(
		middleware.Secure(),
		middleware.BodyLimit("2MB"),
		middleware.CORSWithConfig(middleware.CORSConfig{
			AllowOrigins:  allowOrigins,
			AllowMethods:  allowMethods,
			AllowHeaders:  allowHeaders,
			ExposeHeaders: exposeHeaders,
		}),
	)
	e.Use(middleware.RateLimiter(middleware.NewRateLimiterMemoryStore(rateLimit)))

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
