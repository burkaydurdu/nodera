package v1

import "github.com/labstack/echo/v4"

// Every input arrives through Bind and Validate: path, query and body alike.
func bind(c echo.Context, req any) error {
	if err := c.Bind(req); err != nil {
		return err
	}

	return c.Validate(req)
}
