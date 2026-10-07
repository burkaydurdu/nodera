package v1

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/contexts/storage/aggregates/storage"
	"github.com/piggystats/nodera/contexts/storage/api/v1/middleware"
	"github.com/piggystats/nodera/contexts/storage/application/dto"
)

type IStoragesHandler interface {
	List(ctx context.Context, req *storage.ListRequest) ([]*dto.Storage, error)
	Get(ctx context.Context, req *storage.GetRequest) (*dto.Storage, error)
	Open(ctx context.Context, req *storage.OpenRequest) (*dto.Storage, error)
	ChoosePackage(ctx context.Context, req *storage.ChoosePackageRequest) (*dto.Storage, error)
	Delete(ctx context.Context, req *storage.DeleteRequest) (*dto.Storage, error)
}

type Storages struct {
	handler      IStoragesHandler
	authenticate echo.MiddlewareFunc
}

func NewStorages(handler IStoragesHandler, authenticate echo.MiddlewareFunc) *Storages {
	return &Storages{handler: handler, authenticate: authenticate}
}

func (h *Storages) SetRoutes(group *echo.Group) {
	projects := group.Group("/projects", h.authenticate)
	projects.GET("/:projectID/storages", h.List)
	projects.POST("/:projectID/storages", h.Open)
	projects.GET("/:projectID/storages/:storageID", h.Get)
	projects.DELETE("/:projectID/storages/:storageID", h.Delete)
	projects.PUT("/:projectID/storages/:storageID/package", h.ChoosePackage)
}

// List godoc
// @Summary A Project's Storages
// @Description 403 without storage.read; 404 for a Project the caller is not a member of.
// @Tags storage
// @Security BearerAuth
// @Produce json
// @Param projectID path string true "Project id"
// @Success 200 {array} dto.Storage
// @Router /v1/projects/{projectID}/storages [get]
func (h *Storages) List(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}
	var req storage.ListRequest
	if err := bind(c, &req); err != nil {
		return err
	}
	out, err := h.handler.List(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, out)
}

// Get godoc
// @Summary One Storage
// @Tags storage
// @Security BearerAuth
// @Produce json
// @Param projectID path string true "Project id"
// @Param storageID path string true "Storage id"
// @Success 200 {object} dto.Storage
// @Router /v1/projects/{projectID}/storages/{storageID} [get]
func (h *Storages) Get(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}
	var req storage.GetRequest
	if err := bind(c, &req); err != nil {
		return err
	}
	out, err := h.handler.Get(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, out)
}

// Open godoc
// @Summary Open a Storage
// @Description The Bucket name is 3 to 63 lower-case letters, digits and hyphens, unique in its Region (409 when taken), never changed. Needs a payment method (402) and a Team that is not suspended (423); 403 without storage.write. The Storage is usable when the request returns; 503 when the storage servers could not be reached, and nothing is kept.
// @Tags storage
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param projectID path string true "Project id"
// @Param request body storage.OpenRequest true "Bucket name, Region and Package"
// @Success 201 {object} dto.Storage
// @Router /v1/projects/{projectID}/storages [post]
func (h *Storages) Open(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}
	var req storage.OpenRequest
	if err := bind(c, &req); err != nil {
		return err
	}
	out, err := h.handler.Open(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, out)
}

// ChoosePackage godoc
// @Summary Move a Storage to another Package
// @Description A larger Package applies at once; a smaller one from the next Period, only if the Storage fits it (409 otherwise), and choosing the current Package again undoes it. Needs a payment method (402) and a Team that is not suspended (423); 403 without storage.write.
// @Tags storage
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param projectID path string true "Project id"
// @Param storageID path string true "Storage id"
// @Param request body storage.ChoosePackageRequest true "A Package code"
// @Success 200 {object} dto.Storage
// @Router /v1/projects/{projectID}/storages/{storageID}/package [put]
func (h *Storages) ChoosePackage(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}
	var req storage.ChoosePackageRequest
	if err := bind(c, &req); err != nil {
		return err
	}
	out, err := h.handler.ChoosePackage(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, out)
}

// Delete godoc
// @Summary Delete a Storage and everything in it
// @Description The request repeats the Bucket name (409 when it does not match). The Access keys stop and the Package ends at once; the files go shortly after, and the Storage is shown as deleting until they are gone. 403 without storage.write.
// @Tags storage
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param projectID path string true "Project id"
// @Param storageID path string true "Storage id"
// @Param bucket_name query string true "The Storage's Bucket name, repeated"
// @Success 200 {object} dto.Storage
// @Router /v1/projects/{projectID}/storages/{storageID} [delete]
func (h *Storages) Delete(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}
	var req storage.DeleteRequest
	if err := bind(c, &req); err != nil {
		return err
	}
	out, err := h.handler.Delete(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, out)
}
