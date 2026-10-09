// Package events is the kubernetes context's own view of the payloads it
// exchanges with the customer, billing, droplet and notification contexts:
// the same wire shape, declared here so the contexts never import each other
// (docs/adr/0004, docs/adr/0014).
package events

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// --- customer → kubernetes --------------------------------------------------

type TeamMemberAddedEvent struct {
	Payload struct {
		TeamID  uuid.UUID `json:"team_id"`
		UserID  uuid.UUID `json:"user_id"`
		Role    string    `json:"role"`
		AddedAt time.Time `json:"added_at"`
	} `json:"payload"`
}

type TeamMemberRoleChangedEvent struct {
	Payload struct {
		TeamID    uuid.UUID `json:"team_id"`
		UserID    uuid.UUID `json:"user_id"`
		Role      string    `json:"role"`
		ChangedAt time.Time `json:"changed_at"`
	} `json:"payload"`
}

type TeamMemberRemovedEvent struct {
	Payload struct {
		TeamID    uuid.UUID `json:"team_id"`
		UserID    uuid.UUID `json:"user_id"`
		RemovedAt time.Time `json:"removed_at"`
	} `json:"payload"`
}

type ProjectCreatedEvent struct {
	Payload struct {
		ProjectID uuid.UUID `json:"project_id"`
		TeamID    uuid.UUID `json:"team_id"`
		Name      string    `json:"name"`
		CreatedAt time.Time `json:"created_at"`
	} `json:"payload"`
}

// --- billing → kubernetes ---------------------------------------------------

type TeamBillingStatusChangedEvent struct {
	Payload struct {
		TeamID    uuid.UUID `json:"team_id"`
		Status    string    `json:"status"`
		ChangedAt time.Time `json:"changed_at"`
	} `json:"payload"`
}

type TeamDropletsDeletionRequestedEvent struct {
	Payload struct {
		TeamID      uuid.UUID `json:"team_id"`
		RequestedAt time.Time `json:"requested_at"`
	} `json:"payload"`
}

// --- droplet → kubernetes ---------------------------------------------------

// DropletStatusChangedEvent is one move of a Droplet's status; only the ones
// carrying a node_id concern this context.
type DropletStatusChangedEvent struct {
	Payload struct {
		DropletID     uuid.UUID  `json:"droplet_id"`
		TeamID        uuid.UUID  `json:"team_id"`
		ProjectID     uuid.UUID  `json:"project_id"`
		Hostname      string     `json:"hostname"`
		IPv4          string     `json:"ipv4"`
		FromStatus    *string    `json:"from_status"`
		Status        string     `json:"status"`
		FailureCode   string     `json:"failure_code"`
		FailureReason string     `json:"failure_reason"`
		NodeID        *uuid.UUID `json:"node_id"`
		ClusterID     *uuid.UUID `json:"cluster_id"`
		ChangedAt     time.Time  `json:"changed_at"`
	} `json:"payload"`
}

type NodeRejectedEvent struct {
	Payload struct {
		NodeID     uuid.UUID `json:"node_id"`
		ClusterID  uuid.UUID `json:"cluster_id"`
		Reason     string    `json:"reason"`
		RejectedAt time.Time `json:"rejected_at"`
	} `json:"payload"`
}

type CatalogPlanChangedEvent struct {
	Payload struct {
		Code         string    `json:"code"`
		RegionCode   string    `json:"region_code"`
		CPUCount     int       `json:"cpu_count"`
		RAMGB        int       `json:"ram_gb"`
		HourlyPrice  string    `json:"hourly_price"`
		MonthlyPrice string    `json:"monthly_price"`
		SoldOut      bool      `json:"sold_out"`
		Enabled      bool      `json:"enabled"`
		SyncedAt     time.Time `json:"synced_at"`
	} `json:"payload"`
}

type CatalogRegionChangedEvent struct {
	Payload struct {
		Code     string    `json:"code"`
		City     string    `json:"city"`
		Country  string    `json:"country"`
		Enabled  bool      `json:"enabled"`
		SyncedAt time.Time `json:"synced_at"`
	} `json:"payload"`
}

// --- kubernetes → droplet ---------------------------------------------------

// NodeRequestedBody asks the droplet context for a Node's Droplet
// (docs/adr/0014), by catalog codes; the cloud-init waits in Redis.
type NodeRequestedBody struct {
	NodeID      uuid.UUID `json:"node_id"`
	ClusterID   uuid.UUID `json:"cluster_id"`
	Pool        string    `json:"pool"`
	ProjectID   uuid.UUID `json:"project_id"`
	TeamID      uuid.UUID `json:"team_id"`
	Plan        string    `json:"plan"`
	Region      string    `json:"region"`
	Image       string    `json:"image"`
	Hostname    string    `json:"hostname"`
	RequestedAt time.Time `json:"requested_at"`
}

type NodeRequestedEvent struct {
	Payload NodeRequestedBody `json:"payload"`
}

type NodeReleasedBody struct {
	NodeID     uuid.UUID `json:"node_id"`
	ReleasedAt time.Time `json:"released_at"`
}

type NodeReleasedEvent struct {
	Payload NodeReleasedBody `json:"payload"`
}

// --- kubernetes → notification ----------------------------------------------

// ClusterStatusChangedBody is one move of a Cluster's status; FromStatus is
// nil for a Cluster just opened. The failure fields are set only for a
// failure. It names the Team, the Project and the Cluster and never a
// recipient (docs/adr/0008).
type ClusterStatusChangedBody struct {
	ClusterID     uuid.UUID `json:"cluster_id"`
	TeamID        uuid.UUID `json:"team_id"`
	ProjectID     uuid.UUID `json:"project_id"`
	ProjectName   string    `json:"project_name"`
	Name          string    `json:"name"`
	Region        string    `json:"region"`
	FromStatus    *string   `json:"from_status"`
	Status        string    `json:"status"`
	FailureCode   string    `json:"failure_code,omitempty"`
	FailureReason string    `json:"failure_reason,omitempty"`
	ChangedAt     time.Time `json:"changed_at"`
}

type ClusterStatusChangedEvent struct {
	Payload ClusterStatusChangedBody `json:"payload"`
}

func ToJSONString(event any) (string, error) {
	data, err := json.Marshal(event)
	if err != nil {
		return "", err
	}

	return string(data), nil
}
