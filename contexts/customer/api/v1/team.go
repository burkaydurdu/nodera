package v1

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/contexts/customer/aggregates/project"
	"github.com/piggystats/nodera/contexts/customer/aggregates/team"
	"github.com/piggystats/nodera/contexts/customer/api/v1/middleware"
	"github.com/piggystats/nodera/contexts/customer/application/dto"
)

type ITeamHandler interface {
	ListMine(ctx context.Context) ([]*dto.Team, error)
	Create(ctx context.Context, req *team.CreateRequest) (*dto.Team, error)
	ListProjects(ctx context.Context, req *project.ListRequest) ([]*dto.Project, error)
	CreateProject(ctx context.Context, req *project.CreateRequest) (*dto.Project, error)
	ListMembers(ctx context.Context, req *team.ListMembersRequest) ([]*dto.Member, error)
	ChangeMemberRole(ctx context.Context, req *team.ChangeRoleRequest) (*dto.Member, error)
	RemoveMember(ctx context.Context, req *team.RemoveMemberRequest) error
	Leave(ctx context.Context, req *team.LeaveRequest) error
}

type Team struct {
	handler      ITeamHandler
	authenticate echo.MiddlewareFunc
}

func NewTeam(handler ITeamHandler, authenticate echo.MiddlewareFunc) *Team {
	return &Team{handler: handler, authenticate: authenticate}
}

func (t *Team) SetRoutes(group *echo.Group) {
	teams := group.Group("/teams", t.authenticate)
	teams.GET("", t.ListMine)
	teams.POST("", t.Create)
	teams.GET("/:teamID/projects", t.ListProjects)
	teams.POST("/:teamID/projects", t.CreateProject)
	teams.GET("/:teamID/members", t.ListMembers)
	teams.PUT("/:teamID/members/:userID/role", t.ChangeMemberRole)
	teams.DELETE("/:teamID/members/:userID", t.RemoveMember)
	teams.POST("/:teamID/members/leave", t.Leave)
}

// ListMine godoc
// @Summary The caller's Teams
// @Tags teams
// @Security BearerAuth
// @Produce json
// @Success 200 {array} dto.Team
// @Router /v1/teams [get]
func (t *Team) ListMine(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	teams, err := t.handler.ListMine(ctx)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, teams)
}

// Create godoc
// @Summary Create a Team owned by the caller
// @Tags teams
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param request body team.CreateRequest true "Team"
// @Success 201 {object} dto.Team
// @Router /v1/teams [post]
func (t *Team) Create(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req team.CreateRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	created, err := t.handler.Create(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, created)
}

// ListProjects godoc
// @Summary The Projects of a Team the caller belongs to
// @Tags teams
// @Security BearerAuth
// @Produce json
// @Param teamID path string true "Team id"
// @Success 200 {array} dto.Project
// @Router /v1/teams/{teamID}/projects [get]
func (t *Team) ListProjects(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req project.ListRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	projects, err := t.handler.ListProjects(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, projects)
}

// CreateProject godoc
// @Summary Create a Project in a Team the caller belongs to
// @Tags teams
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param teamID path string true "Team id"
// @Param request body project.CreateRequest true "Project"
// @Success 201 {object} dto.Project
// @Router /v1/teams/{teamID}/projects [post]
func (t *Team) CreateProject(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req project.CreateRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	p, err := t.handler.CreateProject(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, p)
}

// ListMembers godoc
// @Summary The members of a Team the caller belongs to, with their Roles
// @Tags teams
// @Security BearerAuth
// @Produce json
// @Param teamID path string true "Team id"
// @Success 200 {array} dto.Member
// @Router /v1/teams/{teamID}/members [get]
func (t *Team) ListMembers(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req team.ListMembersRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	members, err := t.handler.ListMembers(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, members)
}

// ChangeMemberRole godoc
// @Summary Change a member's Role (owners only; not your own; the last owner stays)
// @Tags teams
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param teamID path string true "Team id"
// @Param userID path string true "Member's user id"
// @Param request body team.ChangeRoleRequest true "New Role"
// @Success 200 {object} dto.Member
// @Router /v1/teams/{teamID}/members/{userID}/role [put]
func (t *Team) ChangeMemberRole(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req team.ChangeRoleRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	member, err := t.handler.ChangeMemberRole(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, member)
}

// RemoveMember godoc
// @Summary Remove a member from a Team (owners only; the last owner stays)
// @Tags teams
// @Security BearerAuth
// @Param teamID path string true "Team id"
// @Param userID path string true "Member's user id"
// @Success 204
// @Router /v1/teams/{teamID}/members/{userID} [delete]
func (t *Team) RemoveMember(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req team.RemoveMemberRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	if err := t.handler.RemoveMember(ctx, &req); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}

// Leave godoc
// @Summary Leave a Team (the last owner cannot)
// @Tags teams
// @Security BearerAuth
// @Param teamID path string true "Team id"
// @Success 204
// @Router /v1/teams/{teamID}/members/leave [post]
func (t *Team) Leave(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req team.LeaveRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	if err := t.handler.Leave(ctx, &req); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}
