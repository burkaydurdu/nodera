package http

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/piggystats/rerror"

	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/constants"
	"github.com/piggystats/nodera/utils/str"
)

const HealthPath = "/v1/health"

// RequestLogger logs one line per request. Successful health probes are
// skipped so the kubelet does not drown the real traffic.
func RequestLogger(log logger.ILog) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()
			err := next(c)

			if err == nil && c.Request().URL.Path == HealthPath {
				return nil
			}

			req := c.Request()
			fields := logger.Fields{
				"method":      req.Method,
				"path":        req.URL.Path,
				"status":      statusFor(err, c.Response()),
				"duration_ms": time.Since(start).Milliseconds(),
				"ip":          c.RealIP(),
				"user_agent":  req.UserAgent(),
			}

			if str.IsNotEmpty(req.URL.RawQuery) {
				fields["query"] = req.URL.RawQuery
			}

			if id, ok := c.Get(constants.CorrelationID.String()).(string); ok && str.IsNotEmpty(id) {
				fields["correlation_id"] = id
			}

			log.InfoWithFields("HTTP Request", fields)

			return err
		}
	}
}

// statusFor answers the status the error handler will write. This middleware
// is the innermost e.Use, so on an error path res.Status is still the default
// 200 when it runs.
func statusFor(err error, res *echo.Response) int {
	if err == nil {
		return res.Status
	}

	if customError, ok := err.(rerror.Error); ok {
		if customError.HttpStatus == 0 {
			return http.StatusBadRequest
		}

		return customError.HttpStatus
	}

	if httpError, ok := err.(*echo.HTTPError); ok {
		return httpError.Code
	}

	return http.StatusInternalServerError
}
