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
	FailureReason       *string          `json:"failure_reason"`
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

	return &Droplet{
		ID:                  d.ID,
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
