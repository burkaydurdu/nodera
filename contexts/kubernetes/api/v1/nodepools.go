package v1

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/contexts/kubernetes/aggregates/cluster"
	"github.com/piggystats/nodera/contexts/kubernetes/api/v1/middleware"
	"github.com/piggystats/nodera/contexts/kubernetes/application/dto"
)

type INodePoolsHandler interface {
	ListNodes(ctx context.Context, req *cluster.ListNodesRequest) ([]dto.Node, error)
	Add(ctx context.Context, req *cluster.AddNodePoolRequest) (*dto.Cluster, error)
	Resize(ctx context.Context, req *cluster.ResizeNodePoolRequest) (*dto.Cluster, error)
	Delete(ctx context.Context, req *cluster.DeleteNodePoolRequest) (*dto.Cluster, error)
}

type NodePools struct {
	handler      INodePoolsHandler
	authenticate echo.MiddlewareFunc
}

func NewNodePools(handler INodePoolsHandler, authenticate echo.MiddlewareFunc) *NodePools {
	return &NodePools{handler: handler, authenticate: authenticate}
}

func (h *NodePools) SetRoutes(group *echo.Group) {
	projects := group.Group("/projects", h.authenticate)
	projects.GET("/:projectID/clusters/:clusterID/nodes", h.ListNodes)
	projects.POST("/:projectID/clusters/:clusterID/node-pools", h.Add)
	projects.PUT("/:projectID/clusters/:clusterID/node-pools/:poolID/count", h.Resize)
	projects.DELETE("/:projectID/clusters/:clusterID/node-pools/:poolID", h.Delete)
}

// ListNodes godoc
// @Summary The Nodes of a Cluster, oldest first
// @Tags kubernetes
// @Security BearerAuth
// @Produce json
// @Param projectID path string true "Project id"
// @Param clusterID path string true "Cluster id"
// @Success 200 {array} dto.Node
// @Router /v1/projects/{projectID}/clusters/{clusterID}/nodes [get]
func (h *NodePools) ListNodes(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}
	var req cluster.ListNodesRequest
	if err := bind(c, &req); err != nil {
		return err
	}
	out, err := h.handler.ListNodes(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, out)
}

// Add godoc
// @Summary Add a Node pool
// @Description Name unique in the Cluster (409), Plan large enough for a Node (400) and in stock (409), count within the limit (400). Needs a payment method (402) and a Team that is not suspended (423); 409 for a Cluster being deleted or failed; 403 without kubernetes.write.
// @Tags kubernetes
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param projectID path string true "Project id"
// @Param clusterID path string true "Cluster id"
// @Param request body cluster.NodePoolInput true "Name, Plan, count, labels and taints"
// @Success 201 {object} dto.Cluster
// @Router /v1/projects/{projectID}/clusters/{clusterID}/node-pools [post]
func (h *NodePools) Add(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}
	var req cluster.AddNodePoolRequest
	if err := bind(c, &req); err != nil {
		return err
	}
	out, err := h.handler.Add(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, out)
}

// Resize godoc
// @Summary Change a Node pool's count
// @Description More Nodes are asked for (needs a payment method, 402, and a Team that is not suspended, 423); fewer releases the newest. 400 outside 1 and the limit; 403 without kubernetes.write.
// @Tags kubernetes
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param projectID path string true "Project id"
// @Param clusterID path string true "Cluster id"
// @Param poolID path string true "Node pool id"
// @Param request body cluster.ResizeNodePoolRequest true "The count"
// @Success 200 {object} dto.Cluster
// @Router /v1/projects/{projectID}/clusters/{clusterID}/node-pools/{poolID}/count [put]
func (h *NodePools) Resize(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}
	var req cluster.ResizeNodePoolRequest
	if err := bind(c, &req); err != nil {
		return err
	}
	out, err := h.handler.Resize(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, out)
}

// Delete godoc
// @Summary Remove a Node pool and release its Nodes
// @Description A Cluster keeps at least one Node pool (409). 403 without kubernetes.write.
// @Tags kubernetes
// @Security BearerAuth
// @Produce json
// @Param projectID path string true "Project id"
// @Param clusterID path string true "Cluster id"
// @Param poolID path string true "Node pool id"
// @Success 200 {object} dto.Cluster
// @Router /v1/projects/{projectID}/clusters/{clusterID}/node-pools/{poolID} [delete]
func (h *NodePools) Delete(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}
	var req cluster.DeleteNodePoolRequest
	if err := bind(c, &req); err != nil {
		return err
	}
	out, err := h.handler.Delete(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, out)
}
