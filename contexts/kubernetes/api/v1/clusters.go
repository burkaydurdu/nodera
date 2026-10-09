package v1

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/contexts/kubernetes/aggregates/cluster"
	"github.com/piggystats/nodera/contexts/kubernetes/api/v1/middleware"
	"github.com/piggystats/nodera/contexts/kubernetes/application/dto"
)

type IClustersHandler interface {
	List(ctx context.Context, req *cluster.ListRequest) ([]*dto.Cluster, error)
	Get(ctx context.Context, req *cluster.GetRequest) (*dto.Cluster, error)
	Open(ctx context.Context, req *cluster.OpenRequest) (*dto.Cluster, error)
	Rename(ctx context.Context, req *cluster.RenameRequest) (*dto.Cluster, error)
	Delete(ctx context.Context, req *cluster.DeleteRequest) (*dto.Cluster, error)
	Kubeconfig(ctx context.Context, req *cluster.KubeconfigRequest) (*dto.Kubeconfig, error)
}

type Clusters struct {
	handler      IClustersHandler
	authenticate echo.MiddlewareFunc
}

func NewClusters(handler IClustersHandler, authenticate echo.MiddlewareFunc) *Clusters {
	return &Clusters{handler: handler, authenticate: authenticate}
}

func (h *Clusters) SetRoutes(group *echo.Group) {
	projects := group.Group("/projects", h.authenticate)
	projects.GET("/:projectID/clusters", h.List)
	projects.POST("/:projectID/clusters", h.Open)
	projects.GET("/:projectID/clusters/:clusterID", h.Get)
	projects.PUT("/:projectID/clusters/:clusterID/name", h.Rename)
	projects.DELETE("/:projectID/clusters/:clusterID", h.Delete)
	projects.GET("/:projectID/clusters/:clusterID/kubeconfig", h.Kubeconfig)
}

// List godoc
// @Summary A Project's Clusters
// @Description 403 without kubernetes.read; 404 for a Project the caller is not a member of.
// @Tags kubernetes
// @Security BearerAuth
// @Produce json
// @Param projectID path string true "Project id"
// @Success 200 {array} dto.Cluster
// @Router /v1/projects/{projectID}/clusters [get]
func (h *Clusters) List(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}
	var req cluster.ListRequest
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
// @Summary One Cluster with its Node pools
// @Tags kubernetes
// @Security BearerAuth
// @Produce json
// @Param projectID path string true "Project id"
// @Param clusterID path string true "Cluster id"
// @Success 200 {object} dto.Cluster
// @Router /v1/projects/{projectID}/clusters/{clusterID} [get]
func (h *Clusters) Get(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}
	var req cluster.GetRequest
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
// @Summary Open a Cluster
// @Description The name is an RFC 1123 label unique in the Project (409 when taken). The Region must sell Kubernetes (400) and the version be offered (400); the first Node pool's Plan must be large enough for a Node (400) and in stock (409). Needs a payment method (402) and a Team that is not suspended (423); 403 without kubernetes.write. The Cluster is answered as provisioning; its Control plane and Nodes follow and arrive through detail.
// @Tags kubernetes
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param projectID path string true "Project id"
// @Param request body cluster.OpenRequest true "Name, Region, version and the first Node pool"
// @Success 201 {object} dto.Cluster
// @Router /v1/projects/{projectID}/clusters [post]
func (h *Clusters) Open(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}
	var req cluster.OpenRequest
	if err := bind(c, &req); err != nil {
		return err
	}
	out, err := h.handler.Open(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, out)
}

// Rename godoc
// @Summary Rename a Cluster
// @Description The name is an RFC 1123 label unique in the Project (409 when taken); the Endpoint does not change. 409 for a Cluster being deleted or failed; 403 without kubernetes.write.
// @Tags kubernetes
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param projectID path string true "Project id"
// @Param clusterID path string true "Cluster id"
// @Param request body cluster.RenameRequest true "The name"
// @Success 200 {object} dto.Cluster
// @Router /v1/projects/{projectID}/clusters/{clusterID}/name [put]
func (h *Clusters) Rename(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}
	var req cluster.RenameRequest
	if err := bind(c, &req); err != nil {
		return err
	}
	out, err := h.handler.Rename(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, out)
}

// Delete godoc
// @Summary Delete a Cluster
// @Description Every Node is released and its Droplet deleted, then the Control plane goes; the Cluster is shown as deleting until then. Allowed whatever the Team's Billing status; 403 without kubernetes.write.
// @Tags kubernetes
// @Security BearerAuth
// @Produce json
// @Param projectID path string true "Project id"
// @Param clusterID path string true "Cluster id"
// @Success 200 {object} dto.Cluster
// @Router /v1/projects/{projectID}/clusters/{clusterID} [delete]
func (h *Clusters) Delete(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}
	var req cluster.DeleteRequest
	if err := bind(c, &req); err != nil {
		return err
	}
	out, err := h.handler.Delete(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, out)
}

// Kubeconfig godoc
// @Summary A Kubeconfig for the caller, good for seven days
// @Description A client certificate cut for the caller as cluster administrator; nothing is kept, so a removed member keeps it until it expires. 409 while the Control plane is not ready, being deleted or failed; 403 without kubernetes.write.
// @Tags kubernetes
// @Security BearerAuth
// @Produce json
// @Param projectID path string true "Project id"
// @Param clusterID path string true "Cluster id"
// @Success 200 {object} dto.Kubeconfig
// @Router /v1/projects/{projectID}/clusters/{clusterID}/kubeconfig [get]
func (h *Clusters) Kubeconfig(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}
	var req cluster.KubeconfigRequest
	if err := bind(c, &req); err != nil {
		return err
	}
	out, err := h.handler.Kubeconfig(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, out)
}
