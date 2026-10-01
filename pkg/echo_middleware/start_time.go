package echo_middleware

import (
	"time"

	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/pkg/constants"
)

func SetStartTime(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		c.Set(constants.StartTime.String(), time.Now())

		return next(c)
	}
}
