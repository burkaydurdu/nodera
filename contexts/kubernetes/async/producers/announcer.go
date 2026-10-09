package producers

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/kubernetes/aggregates/access"
	"github.com/piggystats/nodera/contexts/kubernetes/aggregates/cluster"
	"github.com/piggystats/nodera/contexts/kubernetes/async/constants"
	"github.com/piggystats/nodera/contexts/kubernetes/async/events"
	"github.com/piggystats/nodera/pkg/authz"
	"github.com/piggystats/nodera/pkg/realtime"
)

// Announcer publishes everything this context tells others. Timestamps go out
// in UTC so every consumer reads the same instant whatever zone the database
// handed back.
type Announcer struct {
	client OutboxClient
}

func NewAnnouncer(client OutboxClient) *Announcer {
	return &Announcer{client: client}
}

// NodeRequested asks the droplet context for the Node's Droplet; image is the
// Image every Node runs.
func (a *Announcer) NodeRequested(ctx context.Context, c *cluster.Cluster, pool *cluster.NodePool, n *cluster.Node, image string, at time.Time) error {
	return publish(ctx, a.client, constants.NodeRequested, &events.NodeRequestedEvent{Payload: events.NodeRequestedBody{
		NodeID: n.ID, ClusterID: c.ID, Pool: pool.Name, ProjectID: c.ProjectID, TeamID: c.TeamID,
		Plan: pool.PlanCode, Region: c.Region, Image: image, Hostname: n.Hostname, RequestedAt: at.UTC(),
	}})
}

func (a *Announcer) NodeReleased(ctx context.Context, nodeID uuid.UUID, at time.Time) error {
	return publish(ctx, a.client, constants.NodeReleased, &events.NodeReleasedEvent{Payload: events.NodeReleasedBody{NodeID: nodeID, ReleasedAt: at.UTC()}})
}

// ClusterStatusChanged tells the notification context what happened to a
// Cluster and the Team's open web apps that it changed.
func (a *Announcer) ClusterStatusChanged(ctx context.Context, project *access.ProjectAccess, c *cluster.Cluster, from *cluster.Status, at time.Time) error {
	body := events.ClusterStatusChangedBody{
		ClusterID: c.ID, TeamID: c.TeamID, ProjectID: c.ProjectID, ProjectName: project.Name, Name: c.Name, Region: c.Region,
		Status: string(c.Status), ChangedAt: at.UTC(),
	}
	if from != nil {
		previous := string(*from)
		body.FromStatus = &previous
	}
	if c.Status == cluster.StatusFailed {
		if c.FailureCode != nil {
			body.FailureCode = string(*c.FailureCode)
		}
		if c.FailureReason != nil {
			body.FailureReason = *c.FailureReason
		}
	}
	if err := publish(ctx, a.client, constants.ClusterStatusChanged, &events.ClusterStatusChangedEvent{Payload: body}); err != nil {
		return err
	}

	return a.ClusterChanged(ctx, c.TeamID, c.ProjectID, c.ID, at)
}

// liveCluster is what the web app refetches by.
type liveCluster struct {
	ProjectID uuid.UUID `json:"project_id"`
	ClusterID uuid.UUID `json:"cluster_id"`
}

// ClusterChanged reaches the open web apps of the Team's members who may see
// Kubernetes (docs/adr/0007); the web app refetches.
func (a *Announcer) ClusterChanged(ctx context.Context, teamID, projectID, clusterID uuid.UUID, at time.Time) error {
	message, err := realtime.New(realtime.ToTeam(teamID, authz.KubernetesRead), constants.RealtimeClusterChanged, liveCluster{ProjectID: projectID, ClusterID: clusterID}, at)
	if err != nil {
		return err
	}

	return publish(ctx, a.client, realtime.RoutingKey, message)
}
