package events

import (
	"time"

	"github.com/google/uuid"
)

// ClusterStatusChangedEvent is the kubernetes context's move of a Cluster's
// status; from_status is null for a Cluster just opened. It names the Team,
// the Project and the Cluster and never a recipient (docs/adr/0008).
type ClusterStatusChangedEvent struct {
	Payload struct {
		ClusterID   uuid.UUID `json:"cluster_id"`
		TeamID      uuid.UUID `json:"team_id"`
		ProjectID   uuid.UUID `json:"project_id"`
		ProjectName string    `json:"project_name"`
		Name        string    `json:"name"`
		Region      string    `json:"region"`
		FromStatus  *string   `json:"from_status"`
		Status      string    `json:"status"`
		FailureCode string    `json:"failure_code"`
		ChangedAt   time.Time `json:"changed_at"`
	} `json:"payload"`
}
