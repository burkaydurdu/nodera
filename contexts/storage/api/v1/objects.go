package v1

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/contexts/storage/aggregates/object"
	"github.com/piggystats/nodera/contexts/storage/api/v1/middleware"
	"github.com/piggystats/nodera/contexts/storage/application/dto"
)

type IObjectsHandler interface {
	List(ctx context.Context, req *object.ListRequest) (*dto.Listing, error)
	Delete(ctx context.Context, req *object.DeleteRequest) error
	DownloadURL(ctx context.Context, req *object.URLRequest) (*dto.PresignedURL, error)
	UploadURL(ctx context.Context, req *object.URLRequest) (*dto.PresignedURL, error)
	Access(ctx context.Context, req *object.AccessRequest) (*dto.ObjectAccess, error)
	SetAccess(ctx context.Context, req *object.SetAccessRequest) (*dto.ObjectAccess, error)
	AllowBrowser(ctx context.Context, req *object.BrowserAccessRequest) error
}

// Objects is the file browser's API.
type Objects struct {
	handler      IObjectsHandler
	authenticate echo.MiddlewareFunc
}

func NewObjects(handler IObjectsHandler, authenticate echo.MiddlewareFunc) *Objects {
	return &Objects{handler: handler, authenticate: authenticate}
}

func (h *Objects) SetRoutes(group *echo.Group) {
	projects := group.Group("/projects", h.authenticate)
	projects.GET("/:projectID/storages/:storageID/objects", h.List)
	projects.POST("/:projectID/storages/:storageID/objects/delete", h.Delete)
	projects.POST("/:projectID/storages/:storageID/objects/download-url", h.DownloadURL)
	projects.POST("/:projectID/storages/:storageID/objects/upload-url", h.UploadURL)
	projects.GET("/:projectID/storages/:storageID/objects/access", h.Access)
	projects.PUT("/:projectID/storages/:storageID/objects/access", h.SetAccess)
	projects.PUT("/:projectID/storages/:storageID/browser-access", h.AllowBrowser)
}

// List godoc
// @Summary One page of a folder of the Storage
// @Description prefix is the folder ("" for the root, "images/" for a folder); cursor continues a listing. 423 while the Storage is stopped.
// @Tags storage
// @Security BearerAuth
// @Produce json
// @Param projectID path string true "Project id"
// @Param storageID path string true "Storage id"
// @Param prefix query string false "Folder"
// @Param cursor query string false "Where the previous page ended"
// @Success 200 {object} dto.Listing
// @Router /v1/projects/{projectID}/storages/{storageID}/objects [get]
func (h *Objects) List(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}
	var req object.ListRequest
	if err := bind(c, &req); err != nil {
		return err
	}
	out, err := h.handler.List(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, out)
}

// Delete godoc
// @Summary Delete up to 1,000 files
// @Tags storage
// @Security BearerAuth
// @Accept json
// @Param projectID path string true "Project id"
// @Param storageID path string true "Storage id"
// @Param request body object.DeleteRequest true "The object keys"
// @Success 204
// @Router /v1/projects/{projectID}/storages/{storageID}/objects/delete [post]
func (h *Objects) Delete(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}
	var req object.DeleteRequest
	if err := bind(c, &req); err != nil {
		return err
	}
	if err := h.handler.Delete(ctx, &req); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}

// DownloadURL godoc
// @Summary A URL the browser downloads one file from, valid five minutes
// @Tags storage
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param projectID path string true "Project id"
// @Param storageID path string true "Storage id"
// @Param request body object.URLRequest true "The object key"
// @Success 200 {object} dto.PresignedURL
// @Router /v1/projects/{projectID}/storages/{storageID}/objects/download-url [post]
func (h *Objects) DownloadURL(c echo.Context) error {
	return h.presign(c, h.handler.DownloadURL)
}

// UploadURL godoc
// @Summary A URL the browser puts one file to, valid fifteen minutes
// @Description The file goes straight to the Storage, never through Nodera; one file is at most 5 GB. content_type is signed into the URL and must be sent with the file. An upload is always private. 403 without storage.write.
// @Tags storage
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param projectID path string true "Project id"
// @Param storageID path string true "Storage id"
// @Param request body object.URLRequest true "The object key"
// @Success 200 {object} dto.PresignedURL
// @Router /v1/projects/{projectID}/storages/{storageID}/objects/upload-url [post]
func (h *Objects) UploadURL(c echo.Context) error {
	return h.presign(c, h.handler.UploadURL)
}

func (h *Objects) presign(c echo.Context, sign func(ctx context.Context, req *object.URLRequest) (*dto.PresignedURL, error)) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}
	var req object.URLRequest
	if err := bind(c, &req); err != nil {
		return err
	}
	out, err := sign(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, out)
}

// Access godoc
// @Summary Whether anyone may read an object without a key
// @Tags storage
// @Security BearerAuth
// @Produce json
// @Param projectID path string true "Project id"
// @Param storageID path string true "Storage id"
// @Param key query string true "The object key"
// @Success 200 {object} dto.ObjectAccess
// @Router /v1/projects/{projectID}/storages/{storageID}/objects/access [get]
func (h *Objects) Access(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}
	var req object.AccessRequest
	if err := bind(c, &req); err != nil {
		return err
	}
	out, err := h.handler.Access(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, out)
}

// SetAccess godoc
// @Summary Let anyone read an object, or take that back
// @Description A published object answers GET at its URL without a key; writing it still needs an Access key. 403 without storage.write; 404 for a key the Storage does not hold.
// @Tags storage
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param projectID path string true "Project id"
// @Param storageID path string true "Storage id"
// @Param request body object.SetAccessRequest true "The object key and whether it is public"
// @Success 200 {object} dto.ObjectAccess
// @Router /v1/projects/{projectID}/storages/{storageID}/objects/access [put]
func (h *Objects) SetAccess(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}
	var req object.SetAccessRequest
	if err := bind(c, &req); err != nil {
		return err
	}
	out, err := h.handler.SetAccess(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, out)
}

// AllowBrowser godoc
// @Summary Put Nodera's CORS rule back so the browser can upload again
// @Tags storage
// @Security BearerAuth
// @Param projectID path string true "Project id"
// @Param storageID path string true "Storage id"
// @Success 204
// @Router /v1/projects/{projectID}/storages/{storageID}/browser-access [put]
func (h *Objects) AllowBrowser(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}
	var req object.BrowserAccessRequest
	if err := bind(c, &req); err != nil {
		return err
	}
	if err := h.handler.AllowBrowser(ctx, &req); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}
