package v1

import (
	"errors"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/piggystats/rerror"
)

// Every input arrives through Bind and Validate: path, query and body alike.
func bind(c echo.Context, req any) error {
	if err := c.Bind(req); err != nil {
		return err
	}

	return c.Validate(req)
}

// withRetryAfter surfaces a rate limit as the Retry-After header; rerror
// fields are not serialized, so the header is how a client learns the wait.
func withRetryAfter(c echo.Context, err error) error {
	var rerr rerror.Error
	if errors.As(err, &rerr) {
		if seconds, ok := rerr.GetFields()["retry_after_seconds"].(int); ok {
			c.Response().Header().Set(echo.HeaderRetryAfter, strconv.Itoa(seconds))
		}
	}

	return err
}
