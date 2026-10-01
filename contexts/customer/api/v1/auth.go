package v1

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/piggystats/rerror"

	"github.com/piggystats/nodera/contexts/customer/aggregates/user"
	"github.com/piggystats/nodera/contexts/customer/api/v1/middleware"
	"github.com/piggystats/nodera/contexts/customer/application/dto"
	"github.com/piggystats/nodera/pkg/echo_middleware"
)

type IAuthHandler interface {
	Register(ctx context.Context, req *user.RegisterRequest) error
	VerifyEmail(ctx context.Context, req *user.VerifyEmailRequest) (*dto.Session, error)
	ResendVerification(ctx context.Context, req *user.ResendVerificationRequest) error
	Login(ctx context.Context, req *user.LoginRequest) (*dto.LoginResponse, error)
	VerifyMFA(ctx context.Context, req *user.MFAVerifyRequest) (*dto.Session, error)
	ResendMFA(ctx context.Context, req *user.MFAResendRequest) error
	Refresh(ctx context.Context, req *user.RefreshRequest) (*dto.Session, error)
	Logout(ctx context.Context) error
	LogoutAll(ctx context.Context) error
	ForgotPassword(ctx context.Context, req *user.ForgotPasswordRequest) error
	ResetPassword(ctx context.Context, req *user.ResetPasswordRequest) error
}

type Auth struct {
	handler      IAuthHandler
	authenticate echo.MiddlewareFunc
}

func NewAuth(handler IAuthHandler, authenticate echo.MiddlewareFunc) *Auth {
	return &Auth{handler: handler, authenticate: authenticate}
}

func (a *Auth) SetRoutes(group *echo.Group) {
	public := group.Group("/auth", echo_middleware.ClientInfo)
	public.POST("/register", a.Register)
	public.POST("/verify-email", a.VerifyEmail)
	public.POST("/verify-email/resend", a.ResendVerification)
	public.POST("/login", a.Login)
	public.POST("/mfa/verify", a.VerifyMFA)
	public.POST("/mfa/resend", a.ResendMFA)
	public.POST("/refresh", a.Refresh)
	public.POST("/forgot-password", a.ForgotPassword)
	public.POST("/reset-password", a.ResetPassword)

	private := group.Group("/auth", a.authenticate)
	private.POST("/logout", a.Logout)
	private.POST("/logout-all", a.LogoutAll)
}

// Register godoc
// @Summary Sign up
// @Tags auth
// @Accept json
// @Produce json
// @Param request body user.RegisterRequest true "Sign-up form"
// @Success 201
// @Router /v1/auth/register [post]
func (a *Auth) Register(c echo.Context) error {
	var req user.RegisterRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	if err := a.handler.Register(c.Request().Context(), &req); err != nil {
		return withRetryAfter(c, err)
	}

	return c.NoContent(http.StatusCreated)
}

// VerifyEmail godoc
// @Summary Verify the sign-up email and open the first session
// @Tags auth
// @Accept json
// @Produce json
// @Param request body user.VerifyEmailRequest true "Email and code"
// @Success 200 {object} dto.Session
// @Router /v1/auth/verify-email [post]
func (a *Auth) VerifyEmail(c echo.Context) error {
	var req user.VerifyEmailRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	s, err := a.handler.VerifyEmail(c.Request().Context(), &req)
	if err != nil {
		return withRetryAfter(c, err)
	}

	return c.JSON(http.StatusOK, s)
}

// ResendVerification godoc
// @Summary Send the verification code again
// @Tags auth
// @Accept json
// @Param request body user.ResendVerificationRequest true "Email"
// @Success 200
// @Router /v1/auth/verify-email/resend [post]
func (a *Auth) ResendVerification(c echo.Context) error {
	var req user.ResendVerificationRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	if err := a.handler.ResendVerification(c.Request().Context(), &req); err != nil {
		return withRetryAfter(c, err)
	}

	return c.NoContent(http.StatusOK)
}

// Login godoc
// @Summary Sign in, step one
// @Tags auth
// @Accept json
// @Produce json
// @Param request body user.LoginRequest true "Credentials"
// @Success 200 {object} dto.LoginResponse
// @Router /v1/auth/login [post]
func (a *Auth) Login(c echo.Context) error {
	var req user.LoginRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	response, err := a.handler.Login(c.Request().Context(), &req)
	if err != nil {
		return withRetryAfter(c, err)
	}

	return c.JSON(http.StatusOK, response)
}

// VerifyMFA godoc
// @Summary Sign in, step two
// @Tags auth
// @Accept json
// @Produce json
// @Param request body user.MFAVerifyRequest true "Challenge and code"
// @Success 200 {object} dto.Session
// @Router /v1/auth/mfa/verify [post]
func (a *Auth) VerifyMFA(c echo.Context) error {
	var req user.MFAVerifyRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	s, err := a.handler.VerifyMFA(c.Request().Context(), &req)
	if err != nil {
		return withRetryAfter(c, err)
	}

	return c.JSON(http.StatusOK, s)
}

// ResendMFA godoc
// @Summary Send the MFA code again
// @Tags auth
// @Accept json
// @Param request body user.MFAResendRequest true "Challenge"
// @Success 200
// @Router /v1/auth/mfa/resend [post]
func (a *Auth) ResendMFA(c echo.Context) error {
	var req user.MFAResendRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	if err := a.handler.ResendMFA(c.Request().Context(), &req); err != nil {
		return withRetryAfter(c, err)
	}

	return c.NoContent(http.StatusOK)
}

// Refresh godoc
// @Summary Rotate the refresh token
// @Tags auth
// @Accept json
// @Produce json
// @Param request body user.RefreshRequest true "Refresh token"
// @Success 200 {object} dto.Session
// @Router /v1/auth/refresh [post]
func (a *Auth) Refresh(c echo.Context) error {
	var req user.RefreshRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	s, err := a.handler.Refresh(c.Request().Context(), &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, s)
}

// Logout godoc
// @Summary End this session
// @Tags auth
// @Security BearerAuth
// @Success 204
// @Router /v1/auth/logout [post]
func (a *Auth) Logout(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	if err := a.handler.Logout(ctx); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}

// LogoutAll godoc
// @Summary End every session of the caller
// @Tags auth
// @Security BearerAuth
// @Success 204
// @Router /v1/auth/logout-all [post]
func (a *Auth) LogoutAll(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	if err := a.handler.LogoutAll(ctx); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}

// ForgotPassword godoc
// @Summary Request a password reset link
// @Tags auth
// @Accept json
// @Param request body user.ForgotPasswordRequest true "Email"
// @Success 200
// @Router /v1/auth/forgot-password [post]
func (a *Auth) ForgotPassword(c echo.Context) error {
	var req user.ForgotPasswordRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	if err := a.handler.ForgotPassword(c.Request().Context(), &req); err != nil {
		return withRetryAfter(c, err)
	}

	return c.NoContent(http.StatusOK)
}

// ResetPassword godoc
// @Summary Set a new password with a reset token
// @Tags auth
// @Accept json
// @Param request body user.ResetPasswordRequest true "Token and password"
// @Success 200
// @Router /v1/auth/reset-password [post]
func (a *Auth) ResetPassword(c echo.Context) error {
	var req user.ResetPasswordRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	if err := a.handler.ResetPassword(c.Request().Context(), &req); err != nil {
		return withRetryAfter(c, err)
	}

	return c.NoContent(http.StatusOK)
}

func bind(c echo.Context, req any) error {
	if err := c.Bind(req); err != nil {
		return err
	}

	return c.Validate(req)
}

// withRetryAfter surfaces a lock or a rate limit as the Retry-After header;
// rerror fields are not serialized, so the header is how a client learns the
// wait.
func withRetryAfter(c echo.Context, err error) error {
	var rerr rerror.Error
	if errors.As(err, &rerr) {
		if seconds, ok := rerr.GetFields()["retry_after_seconds"].(int); ok {
			c.Response().Header().Set(echo.HeaderRetryAfter, strconv.Itoa(seconds))
		}
	}

	return err
}
