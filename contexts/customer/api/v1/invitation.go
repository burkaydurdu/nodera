package v1

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/contexts/customer/aggregates/invitation"
	"github.com/piggystats/nodera/contexts/customer/api/v1/middleware"
	"github.com/piggystats/nodera/contexts/customer/application/dto"
	"github.com/piggystats/nodera/pkg/echo_middleware"
)

type IInvitationHandler interface {
	Create(ctx context.Context, req *invitation.CreateRequest) (*dto.Invitation, error)
	Resend(ctx context.Context, req *invitation.TeamScopedRequest) (*dto.Invitation, error)
	Revoke(ctx context.Context, req *invitation.TeamScopedRequest) error
	ListForTeam(ctx context.Context, req *invitation.ListRequest) ([]*dto.Invitation, error)
	ListMine(ctx context.Context) ([]*dto.Invitation, error)
	AcceptByToken(ctx context.Context, req *invitation.TokenRequest) (*dto.AcceptedInvitation, error)
	DeclineByToken(ctx context.Context, req *invitation.TokenRequest) error
	AcceptByID(ctx context.Context, req *invitation.ByIDRequest) (*dto.AcceptedInvitation, error)
	DeclineByID(ctx context.Context, req *invitation.ByIDRequest) error
	Lookup(ctx context.Context, req *invitation.LookupRequest) (*dto.InvitationLookup, error)
}

type Invitation struct {
	handler      IInvitationHandler
	authenticate echo.MiddlewareFunc
}

func NewInvitation(handler IInvitationHandler, authenticate echo.MiddlewareFunc) *Invitation {
	return &Invitation{handler: handler, authenticate: authenticate}
}

func (i *Invitation) SetRoutes(group *echo.Group) {
	teams := group.Group("/teams/:teamID/invitations", i.authenticate)
	teams.GET("", i.ListForTeam)
	teams.POST("", i.Create)
	teams.POST("/:invitationID/resend", i.Resend)
	teams.DELETE("/:invitationID", i.Revoke)

	mine := group.Group("/users/me/invitations", i.authenticate)
	mine.GET("", i.ListMine)
	mine.POST("/:invitationID/accept", i.AcceptByID)
	mine.POST("/:invitationID/decline", i.DeclineByID)

	// The token routes carry the caller's address for their rate limit; the
	// lookup is the one public read, scoped by the token alone.
	public := group.Group("/invitations", echo_middleware.ClientInfo)
	public.GET("/lookup", i.Lookup)
	private := group.Group("/invitations", echo_middleware.ClientInfo, i.authenticate)
	private.POST("/accept", i.AcceptByToken)
	private.POST("/decline", i.DeclineByToken)
}

// Create godoc
// @Summary Invite an email address to a Team with a Role (owners only)
// @Tags invitations
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param teamID path string true "Team id"
// @Param request body invitation.CreateRequest true "Email and Role"
// @Success 201 {object} dto.Invitation
// @Router /v1/teams/{teamID}/invitations [post]
func (i *Invitation) Create(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req invitation.CreateRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	created, err := i.handler.Create(ctx, &req)
	if err != nil {
		return withRetryAfter(c, err)
	}

	return c.JSON(http.StatusCreated, created)
}

// Resend godoc
// @Summary Send an Invitation again with a fresh link (owners only; 60 s cooldown)
// @Tags invitations
// @Security BearerAuth
// @Produce json
// @Param teamID path string true "Team id"
// @Param invitationID path string true "Invitation id"
// @Success 200 {object} dto.Invitation
// @Router /v1/teams/{teamID}/invitations/{invitationID}/resend [post]
func (i *Invitation) Resend(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req invitation.TeamScopedRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	resent, err := i.handler.Resend(ctx, &req)
	if err != nil {
		return withRetryAfter(c, err)
	}

	return c.JSON(http.StatusOK, resent)
}

// Revoke godoc
// @Summary Revoke a pending Invitation (owners only)
// @Tags invitations
// @Security BearerAuth
// @Param teamID path string true "Team id"
// @Param invitationID path string true "Invitation id"
// @Success 204
// @Router /v1/teams/{teamID}/invitations/{invitationID} [delete]
func (i *Invitation) Revoke(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req invitation.TeamScopedRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	if err := i.handler.Revoke(ctx, &req); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}

// ListForTeam godoc
// @Summary The Invitations of a Team, every status, newest first (owners only)
// @Tags invitations
// @Security BearerAuth
// @Produce json
// @Param teamID path string true "Team id"
// @Success 200 {array} dto.Invitation
// @Router /v1/teams/{teamID}/invitations [get]
func (i *Invitation) ListForTeam(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req invitation.ListRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	invitations, err := i.handler.ListForTeam(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, invitations)
}

// ListMine godoc
// @Summary The pending Invitations to the caller's email address
// @Tags invitations
// @Security BearerAuth
// @Produce json
// @Success 200 {array} dto.Invitation
// @Router /v1/users/me/invitations [get]
func (i *Invitation) ListMine(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	invitations, err := i.handler.ListMine(ctx)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, invitations)
}

// AcceptByID godoc
// @Summary Accept one of the caller's pending Invitations
// @Tags invitations
// @Security BearerAuth
// @Produce json
// @Param invitationID path string true "Invitation id"
// @Success 200 {object} dto.AcceptedInvitation
// @Router /v1/users/me/invitations/{invitationID}/accept [post]
func (i *Invitation) AcceptByID(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req invitation.ByIDRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	accepted, err := i.handler.AcceptByID(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, accepted)
}

// DeclineByID godoc
// @Summary Decline one of the caller's pending Invitations
// @Tags invitations
// @Security BearerAuth
// @Param invitationID path string true "Invitation id"
// @Success 204
// @Router /v1/users/me/invitations/{invitationID}/decline [post]
func (i *Invitation) DeclineByID(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req invitation.ByIDRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	if err := i.handler.DeclineByID(ctx, &req); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}

// AcceptByToken godoc
// @Summary Accept the Invitation an email link carries
// @Tags invitations
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param request body invitation.TokenRequest true "Token from the link"
// @Success 200 {object} dto.AcceptedInvitation
// @Router /v1/invitations/accept [post]
func (i *Invitation) AcceptByToken(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req invitation.TokenRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	accepted, err := i.handler.AcceptByToken(ctx, &req)
	if err != nil {
		return withRetryAfter(c, err)
	}

	return c.JSON(http.StatusOK, accepted)
}

// DeclineByToken godoc
// @Summary Decline the Invitation an email link carries
// @Tags invitations
// @Security BearerAuth
// @Accept json
// @Param request body invitation.TokenRequest true "Token from the link"
// @Success 204
// @Router /v1/invitations/decline [post]
func (i *Invitation) DeclineByToken(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req invitation.TokenRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	if err := i.handler.DeclineByToken(ctx, &req); err != nil {
		return withRetryAfter(c, err)
	}

	return c.NoContent(http.StatusNoContent)
}

// Lookup godoc
// @Summary What an Invitation link is for, without signing in
// @Description Token-scoped public read behind the accept page: Team, Role, inviter, the invited address and its state.
// @Tags invitations
// @Produce json
// @Param token query string true "Token from the link"
// @Success 200 {object} dto.InvitationLookup
// @Router /v1/invitations/lookup [get]
func (i *Invitation) Lookup(c echo.Context) error {
	var req invitation.LookupRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	lookup, err := i.handler.Lookup(c.Request().Context(), &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, lookup)
}
