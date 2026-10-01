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
