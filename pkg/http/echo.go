package http

import (
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"golang.org/x/time/rate"

	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/echo_middleware"
)

const (
	DefaultRateLimit = 20
	defaultBodyLimit = "1MB"
)

// CreateEcho builds the canonical Echo instance for the backend services.
func CreateEcho(log logger.ILog, rateLimit int) *echo.Echo {
	return CreateEchoWithBodyLimit(log, rateLimit, defaultBodyLimit)
}

// CreateEchoWithBodyLimit is CreateEcho for a listener whose requests carry
// more than the usual megabyte, such as a Message with attachments.
func CreateEchoWithBodyLimit(log logger.ILog, rateLimit int, bodyLimit string) *echo.Echo {
	if rateLimit == 0 {
		rateLimit = DefaultRateLimit
	}

	e := echo.New()
	e.HideBanner = true
	// The gateway (a private hop) appends the caller to X-Forwarded-For; a
	// value a public client sent is not trusted, so per-IP budgets hold.
	e.IPExtractor = echo.ExtractIPFromXFFHeader(echo.TrustLoopback(true), echo.TrustLinkLocal(true), echo.TrustPrivateNet(true))
	e.Validator = echo_middleware.NewCustomValidator()
	e.HTTPErrorHandler = echo_middleware.ErrorHandler(log)

	e.Use(middleware.RecoverWithConfig(middleware.RecoverConfig{
		StackSize: 1 << 10,
	}))
	e.Use(
		middleware.Secure(),
		middleware.BodyLimit(bodyLimit),
	)
	e.Use(middleware.RateLimiter(middleware.NewRateLimiterMemoryStore(rate.Limit(rateLimit))))
	e.Use(RequestLogger(log))

	return e
}
