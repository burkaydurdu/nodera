package events

import (
	"time"

	"github.com/google/uuid"
)

// DropletStatusChangedEvent is the droplet context's move of a Droplet's
// status; from_status is null for a Droplet just created.
type DropletStatusChangedEvent struct {
	Payload struct {
		DropletID   uuid.UUID `json:"droplet_id"`
		TeamID      uuid.UUID `json:"team_id"`
		ProjectID   uuid.UUID `json:"project_id"`
		Hostname    string    `json:"hostname"`
		IPv4        string    `json:"ipv4"`
		FromStatus  *string   `json:"from_status"`
		Status      string    `json:"status"`
		FailureCode string    `json:"failure_code"`
		ChangedAt   time.Time `json:"changed_at"`
	} `json:"payload"`
}
