package fake_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/piggystats/nodera/contexts/kubernetes/engine"
	"github.com/piggystats/nodera/contexts/kubernetes/engine/fake"
)

func stored(clusterID uuid.UUID) engine.ControlPlaneSpec {
	return engine.ControlPlaneSpec{ClusterID: clusterID, Hostname: clusterID.String() + ".vno1.k8s.nodera.local", Port: 20004, TunnelPort: 20005}
}

// A development binary's fake forgets its Control planes on a restart while the
// database remembers them: one it never created is answered as ready, at the
// Endpoint the database holds.
func TestSelfRunning_AnswersAClusterItNeverCreatedAsReady(t *testing.T) {
	f := fake.NewSelfRunning(stored)
	ctx := context.Background()
	clusterID := uuid.New()

	status, err := f.ControlPlaneStatus(ctx, clusterID)
	require.NoError(t, err)
	assert.Equal(t, engine.ControlPlaneReady, status.State)

	join, err := f.CreateJoin(ctx, clusterID, time.Hour)
	require.NoError(t, err)
	assert.Equal(t, clusterID.String()+".vno1.k8s.nodera.local:20004", join.Endpoint)

	nodes, err := f.ListNodes(ctx, clusterID)
	require.NoError(t, err)
	assert.Empty(t, nodes)
}

// The suite's fake stays strict: a Cluster it was never asked to create is missing.
func TestNew_KeepsAClusterItNeverCreatedMissing(t *testing.T) {
	f := fake.New()
	ctx := context.Background()
	clusterID := uuid.New()

	status, err := f.ControlPlaneStatus(ctx, clusterID)
	require.NoError(t, err)
	assert.Equal(t, engine.ControlPlaneMissing, status.State)

	_, err = f.CreateJoin(ctx, clusterID, time.Hour)
	assert.ErrorIs(t, err, engine.ErrNotFound)
}

// A Kubeconfig points at the Endpoint it was asked for.
func TestIssueKubeconfig_PointsAtTheServerAskedFor(t *testing.T) {
	f := fake.New()
	ctx := context.Background()
	clusterID := uuid.New()
	require.NoError(t, f.CreateControlPlane(ctx, stored(clusterID)))
	f.Advance()

	raw, err := f.IssueKubeconfig(ctx, clusterID, engine.KubeconfigSpec{User: "u", Server: "https://c.vno1.k8s.nodera.local:20004"})
	require.NoError(t, err)
	assert.Contains(t, string(raw), "server: https://c.vno1.k8s.nodera.local:20004\n")
}
