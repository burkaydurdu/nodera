package api

import (
	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/logger"
	nodhttp "github.com/piggystats/nodera/pkg/http"
)

// IntegrationBodyLimit leaves room for the mail service's largest Message,
// 10 MB once decoded, after base64 and JSON have grown it. Echo reads MB as
// 10^6 bytes.
const IntegrationBodyLimit = "15MB"

// CreateIntegrationEcho builds the integration gateway's Echo instance. Its
// callers are software, not browsers, so there is no CORS, and no per-IP
// limit: the budget is per API key, further down the chain.
func CreateIntegrationEcho(log logger.ILog) *echo.Echo {
	e := newEcho(log, IntegrationBodyLimit)
	e.Use(nodhttp.RequestLogger(log))

	return e
}
