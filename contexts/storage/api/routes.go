package api

import (
	"net/http"

	"github.com/labstack/echo/v4"
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
