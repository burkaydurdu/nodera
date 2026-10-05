package v1

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/contexts/customer/aggregates/apikey"
	"github.com/piggystats/nodera/contexts/customer/api/v1/middleware"
	"github.com/piggystats/nodera/contexts/customer/application/dto"
)

type IAPIKeyHandler interface {
	List(ctx context.Context, req *apikey.ListRequest) ([]*dto.APIKey, error)
	Create(ctx context.Context, req *apikey.CreateRequest) (*dto.CreatedAPIKey, error)
	Revoke(ctx context.Context, req *apikey.RevokeRequest) error
}

type APIKey struct {
	handler      IAPIKeyHandler
	authenticate echo.MiddlewareFunc
}

func NewAPIKey(handler IAPIKeyHandler, authenticate echo.MiddlewareFunc) *APIKey {
	return &APIKey{handler: handler, authenticate: authenticate}
}

func (k *APIKey) SetRoutes(group *echo.Group) {
	keys := group.Group("/teams/:teamID/projects/:projectID/api-keys", k.authenticate)
	keys.GET("", k.List)
	keys.POST("", k.Create)
	keys.DELETE("/:keyID", k.Revoke)
}

// List godoc
// @Summary The active API keys of a Project, newest first
// @Description Never shows a key's secret. last_used_at is when the key last authenticated, null if it never has. Needs apikey.write.
// @Tags api-keys
// @Security BearerAuth
// @Produce json
// @Param teamID path string true "Team id"
// @Param projectID path string true "Project id"
// @Success 200 {array} dto.APIKey
// @Router /v1/teams/{teamID}/projects/{projectID}/api-keys [get]
func (k *APIKey) List(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req apikey.ListRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	keys, err := k.handler.List(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, keys)
}

// Create godoc
// @Summary Create an API key for a Project
// @Description Answers the whole key, nd_<id>.<secret>, the only time it is shown. A key carries only Permissions a key may hold (mail.send) and the caller's Role holds; a Project holds at most 20 active keys.
// @Tags api-keys
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param teamID path string true "Team id"
// @Param projectID path string true "Project id"
// @Param request body apikey.CreateRequest true "API key"
// @Success 201 {object} dto.CreatedAPIKey
// @Router /v1/teams/{teamID}/projects/{projectID}/api-keys [post]
func (k *APIKey) Create(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req apikey.CreateRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	created, err := k.handler.Create(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, created)
}

// Revoke godoc
// @Summary Revoke an API key; it is refused from the next request on
// @Tags api-keys
// @Security BearerAuth
// @Param teamID path string true "Team id"
// @Param projectID path string true "Project id"
// @Param keyID path string true "API key id"
// @Success 204
// @Router /v1/teams/{teamID}/projects/{projectID}/api-keys/{keyID} [delete]
func (k *APIKey) Revoke(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req apikey.RevokeRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	if err := k.handler.Revoke(ctx, &req); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}
