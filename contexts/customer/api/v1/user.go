package v1

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/contexts/customer/aggregates/user"
	"github.com/piggystats/nodera/contexts/customer/api/v1/middleware"
	"github.com/piggystats/nodera/contexts/customer/application/dto"
)

type IUserHandler interface {
	GetMe(ctx context.Context) (*dto.User, error)
	UpdateMe(ctx context.Context, req *user.UpdateMeRequest) (*dto.User, error)
	ChangePassword(ctx context.Context, req *user.ChangePasswordRequest) error
	UpdateMFA(ctx context.Context, req *user.UpdateMFARequest) (*dto.User, error)
	ListSessions(ctx context.Context) ([]*dto.SessionItem, error)
	DeleteSession(ctx context.Context, req *user.DeleteSessionRequest) error
}

type User struct {
	handler      IUserHandler
	authenticate echo.MiddlewareFunc
}

func NewUser(handler IUserHandler, authenticate echo.MiddlewareFunc) *User {
	return &User{handler: handler, authenticate: authenticate}
}

func (u *User) SetRoutes(group *echo.Group) {
	users := group.Group("/users", u.authenticate)
	users.GET("/me", u.GetMe)
	users.PUT("/me", u.UpdateMe)
	users.POST("/me/password", u.ChangePassword)
	users.PUT("/me/mfa", u.UpdateMFA)
	users.GET("/me/sessions", u.ListSessions)
	users.DELETE("/me/sessions/:sessionID", u.DeleteSession)
}

// GetMe godoc
// @Summary The caller's profile
// @Tags users
// @Security BearerAuth
// @Produce json
// @Success 200 {object} dto.User
// @Router /v1/users/me [get]
func (u *User) GetMe(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	me, err := u.handler.GetMe(ctx)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, me)
}

// UpdateMe godoc
// @Summary Update name, locale and country
// @Tags users
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param request body user.UpdateMeRequest true "Profile"
// @Success 200 {object} dto.User
// @Router /v1/users/me [put]
func (u *User) UpdateMe(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req user.UpdateMeRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	me, err := u.handler.UpdateMe(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, me)
}

// ChangePassword godoc
// @Summary Change the password; other sessions end
// @Tags users
// @Security BearerAuth
// @Accept json
// @Param request body user.ChangePasswordRequest true "Passwords"
// @Success 204
// @Router /v1/users/me/password [post]
func (u *User) ChangePassword(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req user.ChangePasswordRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	if err := u.handler.ChangePassword(ctx, &req); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}

// UpdateMFA godoc
// @Summary Turn the MFA method on or off
// @Tags users
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param request body user.UpdateMFARequest true "Method and password"
// @Success 200 {object} dto.User
// @Router /v1/users/me/mfa [put]
func (u *User) UpdateMFA(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req user.UpdateMFARequest
	if err := bind(c, &req); err != nil {
		return err
	}

	me, err := u.handler.UpdateMFA(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, me)
}

// ListSessions godoc
// @Summary The caller's live sessions
// @Tags users
// @Security BearerAuth
// @Produce json
// @Success 200 {array} dto.SessionItem
// @Router /v1/users/me/sessions [get]
func (u *User) ListSessions(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	sessions, err := u.handler.ListSessions(ctx)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, sessions)
}

// DeleteSession godoc
// @Summary End one of the caller's sessions
// @Tags users
// @Security BearerAuth
// @Param sessionID path string true "Session id"
// @Success 204
// @Router /v1/users/me/sessions/{sessionID} [delete]
func (u *User) DeleteSession(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req user.DeleteSessionRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	if err := u.handler.DeleteSession(ctx, &req); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}
