// Package dto is the kubernetes context's wire shapes: what the web app is
// answered. The engine's name appears nowhere.
package dto

import (
	"time"

	"github.com/google/uuid"
)

type Version struct {
	Version string `json:"version"`
}

type Region struct {
	Code    string `json:"code"`
	City    string `json:"city"`
	Country string `json:"country"`
}

// Plan is a Plan a Node may have, as the droplet catalog last announced it.
type Plan struct {
	Code         string `json:"code"`
	RegionCode   string `json:"region_code"`
	CPUCount     int    `json:"cpu_count"`
	RAMGB        int    `json:"ram_gb"`
	HourlyPrice  string `json:"hourly_price" example:"0.4920"`
	MonthlyPrice string `json:"monthly_price" example:"330.63"`
	SoldOut      bool   `json:"sold_out"`
}

type Taint struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Effect string `json:"effect" enums:"NoSchedule,PreferNoSchedule,NoExecute"`
}

// NodePool carries its count as asked for; the Nodes say what exists.
type NodePool struct {
	ID        uuid.UUID         `json:"id"`
	Name      string            `json:"name"`
	PlanCode  string            `json:"plan_code"`
	Count     int               `json:"count"`
	Labels    map[string]string `json:"labels"`
	Taints    []Taint           `json:"taints"`
	CreatedAt time.Time         `json:"created_at"`
}

// Node is one member of a Node pool; DropletID names the Droplet it is in
// the Project's Droplet list once the droplet context has opened it.
type Node struct {
	ID            uuid.UUID  `json:"id"`
	PoolID        uuid.UUID  `json:"pool_id"`
	Hostname      string     `json:"hostname"`
	Status        string     `json:"status" enums:"requested,joining,ready,not_ready,releasing,failed"`
	DropletID     *uuid.UUID `json:"droplet_id"`
	DropletStatus string     `json:"droplet_status"`
	IPv4          *string    `json:"ipv4"`
	FailureCode   *string    `json:"failure_code" enums:"node_rejected,timed_out"`
	FailureReason *string    `json:"failure_reason"`
	CreatedAt     time.Time  `json:"created_at"`
	ReadyAt       *time.Time `json:"ready_at"`
}

type Cluster struct {
	ID        uuid.UUID `json:"id"`
	ProjectID uuid.UUID `json:"project_id"`
	Name      string    `json:"name"`
	Region    string    `json:"region"`
	Version   string    `json:"version"`
	Status    string    `json:"status" enums:"provisioning,running,degraded,deleting,failed"`
	// Endpoint is host:port; a Kubeconfig points at https://<endpoint>.
	Endpoint      string     `json:"endpoint" example:"0192f0c4-7d2a-7c3e-9a51-3f8e2b6d1a07.vno1.k8s.nodera.cloud:20000"`
	BillingStatus string     `json:"billing_status"`
	FailureCode   *string    `json:"failure_code" enums:"control_plane_failed"`
	FailureReason *string    `json:"failure_reason"`
	NodePools     []NodePool `json:"node_pools"`
	CreatedAt     time.Time  `json:"created_at"`
	DeletedAt     *time.Time `json:"deleted_at"`
}

// Kubeconfig is the file itself and when the certificate in it stops working.
type Kubeconfig struct {
	Kubeconfig string    `json:"kubeconfig"`
	ExpiresAt  time.Time `json:"expires_at"`
}
