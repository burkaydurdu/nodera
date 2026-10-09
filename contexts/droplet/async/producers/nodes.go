package producers

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/droplet/async/constants"
	"github.com/piggystats/nodera/contexts/droplet/async/events"
)

// NodeRejected tells the kubernetes context that no Droplet could be opened
// for a Node it asked for (docs/adr/0014).
type NodeRejected struct{ client OutboxClient }

func NewNodeRejected(client OutboxClient) *NodeRejected {
	return &NodeRejected{client: client}
}

func (p *NodeRejected) Publish(ctx context.Context, nodeID, clusterID uuid.UUID, reason string, at time.Time) error {
	return publish(ctx, p.client, constants.NodeRejected, &events.NodeRejectedEvent{
		Payload: events.NodeRejectedBody{NodeID: nodeID, ClusterID: clusterID, Reason: reason, RejectedAt: at.UTC()},
	})
}
