package echo_middleware

import (
	"net/http"
	"runtime"

	"github.com/go-playground/validator/v10"
	"github.com/labstack/echo/v4"
	"github.com/piggystats/rerror"

	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/apperror"
)

// ErrorHandler renders every error as the platform envelope {code, domain,
// message}. Echo's own HTTPErrors (the gateway's 401/403/404) are wrapped into
// the same shape so one client error routine serves gateway and services.
func ErrorHandler(log logger.ILog) echo.HTTPErrorHandler {
	return func(err error, c echo.Context) {
		if c.Response().Committed {
			return
		}

		if validationErr, ok := err.(validator.ValidationErrors); ok {
			_ = c.JSON(http.StatusBadRequest, GetValidationError(validationErr))

			return
		}

		if customError, ok := err.(rerror.Error); ok {
			status := customError.HttpStatus
			if status == 0 {
				status = http.StatusBadRequest
			}

			_ = c.JSON(status, customError)
			if status >= http.StatusInternalServerError {
				log.Error(customError)
			} else {
				log.Warn(customError.Error())
			}

			return
		}

		if httpError, ok := err.(*echo.HTTPError); ok {
			message, _ := httpError.Message.(string)
			_ = c.JSON(httpError.Code, rerror.Error{
				Code:    httpError.Code,
				Domain:  apperror.Domain,
				Message: message,
			})

			return
		}

		if _, ok := err.(runtime.Error); ok {
			log.Error(apperror.ErrorPanic.Copy().SetError(err))
		} else {
			log.ErrorWithFields(err, logger.Fields{
				"path":   c.Path(),
				"method": c.Request().Method,
			})
		}

		_ = c.JSON(http.StatusInternalServerError, apperror.ErrorGeneric.Copy())
	}
}
