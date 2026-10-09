package handlers

import (
	"context"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/notification/aggregates/notification"
	"github.com/piggystats/nodera/utils/str"
)

// Cluster statuses this context reacts to, as the kubernetes context names
// them.
const (
	clusterRunning = "running"
	clusterFailed  = "failed"

	// unknownClusterFailure words a failure from a producer that sent no code.
	unknownClusterFailure = "control_plane_failed"
)

// ClusterStatusChange is one move of a Cluster's status; From is nil for a
// Cluster just opened.
type ClusterStatusChange struct {
	ClusterID   uuid.UUID
	TeamID      uuid.UUID
	ProjectID   uuid.UUID
	ProjectName string
	Name        string
	From        *string
	Status      string
	FailureCode string
}

// Clusters decides which moves of a Cluster are worth telling the Team about:
// the first time it runs, and when it fails. Every other move only refreshes
// the web app, which the kubernetes context does itself.
type Clusters struct {
	notifier ITeamNotifier
}

func NewClusters(notifier ITeamNotifier) *Clusters {
	return &Clusters{notifier: notifier}
}

func (h *Clusters) StatusChanged(ctx context.Context, change ClusterStatusChange) error {
	params := map[string]any{
		"cluster_id": change.ClusterID.String(), "project_id": change.ProjectID.String(),
		"project_name": change.ProjectName, "name": change.Name,
	}
	switch change.Status {
	case clusterRunning:
		// The source key names the Cluster, so only the first time it runs is
		// recorded: a Cluster losing and regaining its Nodes is no news.
		return h.notifier.NotifyTeam(ctx, TeamEvent{
			SourceKey: string(notification.TypeKubernetesClusterRunning) + ":" + change.ClusterID.String(),
			Type:      notification.TypeKubernetesClusterRunning,
			TeamID:    change.TeamID,
			Params:    params,
		})
	case clusterFailed:
		code := change.FailureCode
		if str.IsBlank(code) {
			code = unknownClusterFailure
		}
		params["failure_code"] = code

		// The reason travels as a code: the web app and the email word it in
		// the reader's language.
		return h.notifier.NotifyTeam(ctx, TeamEvent{
			SourceKey:   string(notification.TypeKubernetesClusterFailed) + ":" + change.ClusterID.String(),
			Type:        notification.TypeKubernetesClusterFailed,
			TeamID:      change.TeamID,
			Params:      params,
			EmailParams: map[string]any{"Name": change.Name, "ProjectName": change.ProjectName, "FailureKey": "kubernetes_failure_" + code},
		})
	default:
		return nil
	}
}
