package cluster

import "strings"

// NodePoolInput is a Node pool as a customer asks for it.
type NodePoolInput struct {
	Name   string            `json:"name" validate:"required,max=32"`
	Plan   string            `json:"plan" validate:"required,max=64"`
	Count  int               `json:"count" validate:"required,min=1"`
	Labels map[string]string `json:"labels" validate:"max=10"`
	Taints []Taint           `json:"taints" validate:"max=10,dive"`
}

func (i *NodePoolInput) Normalize() {
	i.Name = strings.ToLower(strings.TrimSpace(i.Name))
	i.Plan = strings.ToLower(strings.TrimSpace(i.Plan))
	if i.Labels == nil {
		i.Labels = map[string]string{}
	}
	if i.Taints == nil {
		i.Taints = []Taint{}
	}
}

// OpenRequest carries codes, not ids: the customer picked them from the
// catalog. A Cluster is opened with one Node pool.
type OpenRequest struct {
	ProjectID string        `param:"projectID" json:"-" validate:"required,uuid"`
	Name      string        `json:"name" validate:"required,max=63"`
	Region    string        `json:"region" validate:"required,max=16"`
	Version   string        `json:"version" validate:"required,max=16"`
	NodePool  NodePoolInput `json:"node_pool" validate:"required"`
}

func (r *OpenRequest) Normalize() {
	r.Name = strings.ToLower(strings.TrimSpace(r.Name))
	r.Region = strings.ToLower(strings.TrimSpace(r.Region))
	r.Version = strings.TrimPrefix(strings.TrimSpace(r.Version), "v")
	r.NodePool.Normalize()
}

type ListRequest struct {
	ProjectID string `param:"projectID" json:"-" validate:"required,uuid"`
}

type GetRequest struct {
	ProjectID string `param:"projectID" json:"-" validate:"required,uuid"`
	ClusterID string `param:"clusterID" json:"-" validate:"required,uuid"`
}

type RenameRequest struct {
	ProjectID string `param:"projectID" json:"-" validate:"required,uuid"`
	ClusterID string `param:"clusterID" json:"-" validate:"required,uuid"`
	Name      string `json:"name" validate:"required,max=63"`
}

func (r *RenameRequest) Normalize() {
	r.Name = strings.ToLower(strings.TrimSpace(r.Name))
}

type DeleteRequest struct {
	ProjectID string `param:"projectID" json:"-" validate:"required,uuid"`
	ClusterID string `param:"clusterID" json:"-" validate:"required,uuid"`
}

type KubeconfigRequest struct {
	ProjectID string `param:"projectID" json:"-" validate:"required,uuid"`
	ClusterID string `param:"clusterID" json:"-" validate:"required,uuid"`
}

type ListNodesRequest struct {
	ProjectID string `param:"projectID" json:"-" validate:"required,uuid"`
	ClusterID string `param:"clusterID" json:"-" validate:"required,uuid"`
}

type AddNodePoolRequest struct {
	ProjectID string `param:"projectID" json:"-" validate:"required,uuid"`
	ClusterID string `param:"clusterID" json:"-" validate:"required,uuid"`
	NodePoolInput
}

type ResizeNodePoolRequest struct {
	ProjectID string `param:"projectID" json:"-" validate:"required,uuid"`
	ClusterID string `param:"clusterID" json:"-" validate:"required,uuid"`
	PoolID    string `param:"poolID" json:"-" validate:"required,uuid"`
	Count     int    `json:"count" validate:"required,min=1"`
}

type DeleteNodePoolRequest struct {
	ProjectID string `param:"projectID" json:"-" validate:"required,uuid"`
	ClusterID string `param:"clusterID" json:"-" validate:"required,uuid"`
	PoolID    string `param:"poolID" json:"-" validate:"required,uuid"`
}
