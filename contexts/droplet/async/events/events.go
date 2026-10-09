// Package events is the droplet context's own view of the payloads it
// exchanges with the customer and billing contexts: the same wire shape,
// declared here so the contexts never import each other (docs/adr/0004,
// docs/adr/0006).
package events

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type TeamCreatedEvent struct {
	Payload struct {
		TeamID      uuid.UUID `json:"team_id"`
		OwnerUserID uuid.UUID `json:"owner_user_id"`
		CreatedAt   time.Time `json:"created_at"`
	} `json:"payload"`
}

type TeamMemberAddedEvent struct {
	Payload struct {
		TeamID  uuid.UUID `json:"team_id"`
		UserID  uuid.UUID `json:"user_id"`
		Role    string    `json:"role"`
		AddedAt time.Time `json:"added_at"`
	} `json:"payload"`
}

type ProjectCreatedEvent struct {
	Payload struct {
		ProjectID uuid.UUID `json:"project_id"`
		TeamID    uuid.UUID `json:"team_id"`
		CreatedAt time.Time `json:"created_at"`
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

// --- billing → droplet ------------------------------------------------------

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

// --- kubernetes → droplet ---------------------------------------------------

// NodeRequestedEvent asks for a Node's Droplet (docs/adr/0014). It names the
// catalog by codes, as a customer's request does; the cloud-init waits in
// Redis, never in the event.
type NodeRequestedEvent struct {
	Payload struct {
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
	} `json:"payload"`
}

type NodeReleasedEvent struct {
	Payload struct {
		NodeID     uuid.UUID `json:"node_id"`
		ReleasedAt time.Time `json:"released_at"`
	} `json:"payload"`
}

// --- droplet → kubernetes ---------------------------------------------------

// NodeRejectedBody is a request no Droplet could be opened for: the catalog
// does not sell what was asked, the Team may not be charged, or the join
// secret had expired. Reason is the customer-safe sentence.
type NodeRejectedBody struct {
	NodeID     uuid.UUID `json:"node_id"`
	ClusterID  uuid.UUID `json:"cluster_id"`
	Reason     string    `json:"reason"`
	RejectedAt time.Time `json:"rejected_at"`
}

type NodeRejectedEvent struct {
	Payload NodeRejectedBody `json:"payload"`
}

// CatalogPlanChangedBody is one Plan as the catalog holds it now; prices are
// decimal strings. The kubernetes context keeps the newest by synced_at.
type CatalogPlanChangedBody struct {
	Code         string    `json:"code"`
	RegionCode   string    `json:"region_code"`
	CPUCount     int       `json:"cpu_count"`
	RAMGB        int       `json:"ram_gb"`
	HourlyPrice  string    `json:"hourly_price"`
	MonthlyPrice string    `json:"monthly_price"`
	SoldOut      bool      `json:"sold_out"`
	Enabled      bool      `json:"enabled"`
	SyncedAt     time.Time `json:"synced_at"`
}

type CatalogPlanChangedEvent struct {
	Payload CatalogPlanChangedBody `json:"payload"`
}

type CatalogRegionChangedBody struct {
	Code     string    `json:"code"`
	City     string    `json:"city"`
	Country  string    `json:"country"`
	Enabled  bool      `json:"enabled"`
	SyncedAt time.Time `json:"synced_at"`
}

type CatalogRegionChangedEvent struct {
	Payload CatalogRegionChangedBody `json:"payload"`
}

// --- droplet → billing ------------------------------------------------------

// DropletBillingStartedBody carries the Locked price as a 4-decimal string;
// money never travels as a float.
type DropletBillingStartedBody struct {
	DropletID   uuid.UUID `json:"droplet_id"`
	TeamID      uuid.UUID `json:"team_id"`
	ProjectID   uuid.UUID `json:"project_id"`
	Hostname    string    `json:"hostname"`
	PlanCode    string    `json:"plan_code"`
	HourlyPrice string    `json:"hourly_price"`
	StartedAt   time.Time `json:"started_at"`
}

type DropletBillingStartedEvent struct {
	Payload DropletBillingStartedBody `json:"payload"`
}

type DropletBillingStoppedBody struct {
	DropletID uuid.UUID `json:"droplet_id"`
	TeamID    uuid.UUID `json:"team_id"`
	StoppedAt time.Time `json:"stopped_at"`
}

type DropletBillingStoppedEvent struct {
	Payload DropletBillingStoppedBody `json:"payload"`
}

// --- droplet → notification ------------------------------------------------

// DropletStatusChangedBody is one move of a Droplet's status; FromStatus is
// nil for a Droplet just created. The failure fields are set only for a
// failure: the code for words in the reader's language, the reason as the
// customer-safe English sentence. NodeID and ClusterID are set for a Node
// Droplet, so the kubernetes context follows its Nodes by them (docs/adr/0014).
type DropletStatusChangedBody struct {
	DropletID     uuid.UUID  `json:"droplet_id"`
	TeamID        uuid.UUID  `json:"team_id"`
	ProjectID     uuid.UUID  `json:"project_id"`
	Hostname      string     `json:"hostname"`
	IPv4          string     `json:"ipv4"`
	FromStatus    *string    `json:"from_status"`
	Status        string     `json:"status"`
	FailureCode   string     `json:"failure_code,omitempty"`
	FailureReason string     `json:"failure_reason,omitempty"`
	NodeID        *uuid.UUID `json:"node_id,omitempty"`
	ClusterID     *uuid.UUID `json:"cluster_id,omitempty"`
	ChangedAt     time.Time  `json:"changed_at"`
}

type DropletStatusChangedEvent struct {
	Payload DropletStatusChangedBody `json:"payload"`
}

func ToJSONString(event any) (string, error) {
	data, err := json.Marshal(event)
	if err != nil {
		return "", err
	}

	return string(data), nil
}
