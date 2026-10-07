package v1

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/contexts/storage/aggregates/accesskey"
	"github.com/piggystats/nodera/contexts/storage/api/v1/middleware"
	"github.com/piggystats/nodera/contexts/storage/application/dto"
)

type IAccessKeysHandler interface {
	List(ctx context.Context, req *accesskey.ListRequest) ([]dto.AccessKey, error)
	Create(ctx context.Context, req *accesskey.CreateRequest) (*dto.CreatedAccessKey, error)
	Revoke(ctx context.Context, req *accesskey.RevokeRequest) error
}

type AccessKeys struct {
	handler      IAccessKeysHandler
	authenticate echo.MiddlewareFunc
}

func NewAccessKeys(handler IAccessKeysHandler, authenticate echo.MiddlewareFunc) *AccessKeys {
	return &AccessKeys{handler: handler, authenticate: authenticate}
}

func (h *AccessKeys) SetRoutes(group *echo.Group) {
	projects := group.Group("/projects", h.authenticate)
	projects.GET("/:projectID/storages/:storageID/access-keys", h.List)
	projects.POST("/:projectID/storages/:storageID/access-keys", h.Create)
	projects.DELETE("/:projectID/storages/:storageID/access-keys/:accessKeyID", h.Revoke)
}

// List godoc
// @Summary A Storage's Access keys, oldest first
// @Tags storage
// @Security BearerAuth
// @Produce json
// @Param projectID path string true "Project id"
// @Param storageID path string true "Storage id"
// @Success 200 {array} dto.AccessKey
// @Router /v1/projects/{projectID}/storages/{storageID}/access-keys [get]
func (h *AccessKeys) List(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}
	var req accesskey.ListRequest
	if err := bind(c, &req); err != nil {
		return err
	}
	out, err := h.handler.List(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, out)
}

// Create godoc
// @Summary Make an Access key for a Storage
// @Description The secret key is in this answer and nowhere else. A Storage holds at most 20 (409); a Paused Storage or a suspended Team's takes none (423). 403 without storage.write.
// @Tags storage
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param projectID path string true "Project id"
// @Param storageID path string true "Storage id"
// @Param request body accesskey.CreateRequest true "A name and read or read_write"
// @Success 201 {object} dto.CreatedAccessKey
// @Router /v1/projects/{projectID}/storages/{storageID}/access-keys [post]
func (h *AccessKeys) Create(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}
	var req accesskey.CreateRequest
	if err := bind(c, &req); err != nil {
		return err
	}
	out, err := h.handler.Create(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, out)
}

// Revoke godoc
// @Summary Revoke an Access key
// @Description The key is refused on its next request. 503 when the storage servers could not be reached; the key then stays listed.
// @Tags storage
// @Security BearerAuth
// @Param projectID path string true "Project id"
// @Param storageID path string true "Storage id"
// @Param accessKeyID path string true "Access key id"
// @Success 204
// @Router /v1/projects/{projectID}/storages/{storageID}/access-keys/{accessKeyID} [delete]
func (h *AccessKeys) Revoke(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}
	var req accesskey.RevokeRequest
	if err := bind(c, &req); err != nil {
		return err
	}
	if err := h.handler.Revoke(ctx, &req); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}
