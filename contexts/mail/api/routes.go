package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/piggystats/rerror"
	echoSwagger "github.com/swaggo/echo-swagger"
)

func NewV1Routes(e *echo.Echo) *echo.Group {
	v1 := e.Group("/v1")
	v1.GET("/health", func(c echo.Context) error {
		return c.JSON(http.StatusOK, "OK")
	})

	return v1
}

func InitSwagger(e *echo.Echo) {
	e.GET("/swagger/*", echoSwagger.WrapHandler)
}

// WithRetryAfter surfaces a wait as the Retry-After header; rerror fields
// are not serialized, so the header is how a client learns it.
func WithRetryAfter(c echo.Context, err error) error {
	var rerr rerror.Error
	if errors.As(err, &rerr) {
		if seconds, ok := rerr.GetFields()["retry_after_seconds"].(int); ok {
			c.Response().Header().Set(echo.HeaderRetryAfter, strconv.Itoa(seconds))
		}
	}

	return err
}
