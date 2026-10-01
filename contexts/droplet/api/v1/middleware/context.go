package middleware

import (
	"context"

	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/pkg/apperror"
	"github.com/piggystats/nodera/pkg/constants"
	"github.com/piggystats/nodera/utils/str"
)

// GetContext copies the authenticated request's invariants onto the Go
// context the handlers receive.
func GetContext(c echo.Context) (context.Context, error) {
	correlationID, ok := c.Get(constants.CorrelationID.String()).(string)
	if !ok {
		return nil, apperror.ErrorMissingCorrelationID
	}

	userID, ok := c.Get(constants.CurrentUserID.String()).(string)
	if !ok || str.IsEmpty(userID) {
		return nil, apperror.ErrorMissingUserID
	}

	sessionID, ok := c.Get(constants.CurrentSessionID.String()).(string)
	if !ok || str.IsEmpty(sessionID) {
		return nil, apperror.ErrorMissingSessionID
	}

	email, _ := c.Get(constants.CurrentUserEmail.String()).(string)
	status, _ := c.Get(constants.CurrentUserStatus.String()).(string)

	ctx := c.Request().Context()
	ctx = context.WithValue(ctx, constants.CorrelationID, correlationID)
	ctx = context.WithValue(ctx, constants.CurrentUserID, userID)
	ctx = context.WithValue(ctx, constants.CurrentSessionID, sessionID)
	ctx = context.WithValue(ctx, constants.CurrentUserEmail, email)
	ctx = context.WithValue(ctx, constants.CurrentUserStatus, status)

	return ctx, nil
}
