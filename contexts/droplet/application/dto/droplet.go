package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/droplet/aggregates/catalog"
	"github.com/piggystats/nodera/contexts/droplet/aggregates/droplet"
)

type DropletSSHKey struct {
	Name        string `json:"name"`
	Fingerprint string `json:"fingerprint"`
}

type DropletHistory struct {
	From   *string   `json:"from"`
	To     string    `json:"to"`
	At     time.Time `json:"at"`
	Reason string    `json:"reason"`
}

// DropletNode says which Cluster and Node pool a Node Droplet belongs to; it
// is null for a Droplet a customer opened. A Node Droplet takes no power
// action and no deletion here: its Cluster releases it.
type DropletNode struct {
	ClusterID uuid.UUID `json:"cluster_id"`
	NodeID    uuid.UUID `json:"node_id"`
	Pool      string    `json:"pool"`
}

type Droplet struct {
	ID                  uuid.UUID        `json:"id"`
	ProjectID           uuid.UUID        `json:"project_id"`
	Hostname            string           `json:"hostname"`
	Status              string           `json:"status" enums:"provisioning,active,off,failed,deleting,deleted"`
	PlanCode            string           `json:"plan_code"`
	RegionCode          string           `json:"region_code"`
	ImageCode           string           `json:"image_code"`
	IPv4                *string          `json:"ipv4"`
	IPv6                *string          `json:"ipv6"`
	HourlyPrice         string           `json:"hourly_price" example:"0.4920"`
	MonthlyPrice        string           `json:"monthly_price" example:"330.63"`
	SSHKeys             []DropletSSHKey  `json:"ssh_keys"`
	Node                *DropletNode     `json:"node"`
	FailureReason       *string          `json:"failure_reason"`
	FailureCode         *string          `json:"failure_code" enums:"rejected,unavailable,timed_out,server_gone,provider_failed"`
	CreatedAt           time.Time        `json:"created_at"`
	ActiveAt            *time.Time       `json:"active_at"`
	DeletionRequestedAt *time.Time       `json:"deletion_requested_at"`
	History             []DropletHistory `json:"history,omitempty"`
}

func ToDroplet(d *droplet.Droplet) *Droplet {
	keys := make([]DropletSSHKey, 0, len(d.SSHKeys))
	for _, k := range d.SSHKeys {
		keys = append(keys, DropletSSHKey{Name: k.Name, Fingerprint: k.Fingerprint})
	}

	var node *DropletNode
	if d.Node != nil {
		node = &DropletNode{ClusterID: d.Node.ClusterID, NodeID: d.Node.NodeID, Pool: d.Node.Pool}
	}

	return &Droplet{
		ID:                  d.ID,
		Node:                node,
		ProjectID:           d.ProjectID,
		Hostname:            d.Hostname,
		Status:              string(d.Status),
		PlanCode:            d.PlanCode,
		RegionCode:          d.RegionCode,
		ImageCode:           d.ImageCode,
		IPv4:                d.IPv4,
		IPv6:                d.IPv6,
		HourlyPrice:         d.HourlyPrice.StringFixed(catalog.HourlyScale),
		MonthlyPrice:        catalog.MonthlyPrice(d.HourlyPrice, d.MonthlyCapHours).StringFixed(catalog.MonthlyScale),
		SSHKeys:             keys,
		FailureReason:       d.FailureReason,
		FailureCode:         failureCode(d.FailureCode),
		CreatedAt:           d.CreatedAt,
		ActiveAt:            d.ActiveAt,
		DeletionRequestedAt: d.DeletionRequest,
	}
}

// ToDropletWithHistory is the detail shape; the list never carries history.
func ToDropletWithHistory(d *droplet.Droplet, history droplet.History) *Droplet {
	out := ToDroplet(d)
	out.History = make([]DropletHistory, 0, len(history))
	for _, h := range history {
		entry := DropletHistory{To: string(h.To), At: h.At, Reason: h.Reason}
		if h.From != nil {
			from := string(*h.From)
			entry.From = &from
		}
		out.History = append(out.History, entry)
	}

	return out
}

func ToDroplets(droplets droplet.Droplets) []*Droplet {
	out := make([]*Droplet, 0, len(droplets))
	for _, d := range droplets {
		out = append(out, ToDroplet(d))
	}

	return out
}

func failureCode(code *droplet.FailureCode) *string {
	if code == nil {
		return nil
	}
	value := string(*code)

	return &value
}
