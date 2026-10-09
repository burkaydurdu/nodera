package cluster_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/piggystats/nodera/contexts/kubernetes/aggregates/cluster"
)

func TestStatus_TransitionsFollowTheTable(t *testing.T) {
	assert.True(t, cluster.StatusProvisioning.CanTransitionTo(cluster.StatusRunning))
	assert.True(t, cluster.StatusProvisioning.CanTransitionTo(cluster.StatusDegraded))
	assert.True(t, cluster.StatusRunning.CanTransitionTo(cluster.StatusDegraded))
	assert.True(t, cluster.StatusDegraded.CanTransitionTo(cluster.StatusRunning))
	assert.True(t, cluster.StatusFailed.CanTransitionTo(cluster.StatusDeleting))
	assert.False(t, cluster.StatusFailed.CanTransitionTo(cluster.StatusRunning), "a failed Cluster is only ever deleted")
	assert.False(t, cluster.StatusDeleting.CanTransitionTo(cluster.StatusRunning))
	assert.False(t, cluster.StatusDeleted.CanTransitionTo(cluster.StatusDeleting))
}

func TestStatus_AllowsChangeOnlyWhileAlive(t *testing.T) {
	for _, s := range []cluster.Status{cluster.StatusProvisioning, cluster.StatusRunning, cluster.StatusDegraded} {
		assert.True(t, s.AllowsChange(), s)
	}
	for _, s := range []cluster.Status{cluster.StatusFailed, cluster.StatusDeleting, cluster.StatusDeleted} {
		assert.False(t, s.AllowsChange(), s)
	}
}

func TestNodeStatus_CountsTowardsThePool(t *testing.T) {
	for _, s := range []cluster.NodeStatus{cluster.NodeRequested, cluster.NodeJoining, cluster.NodeReady, cluster.NodeNotReady} {
		assert.True(t, s.Counts(), s)
	}
	for _, s := range []cluster.NodeStatus{cluster.NodeReleasing, cluster.NodeGone, cluster.NodeFailed} {
		assert.False(t, s.Counts(), s)
	}
}

func TestValidName_IsAnRFC1123Label(t *testing.T) {
	assert.True(t, cluster.ValidName("prod", cluster.MaxNameLength))
	assert.True(t, cluster.ValidName("prod-eu-1", cluster.MaxNameLength))
	assert.False(t, cluster.ValidName("Prod", cluster.MaxNameLength))
	assert.False(t, cluster.ValidName("-prod", cluster.MaxNameLength))
	assert.False(t, cluster.ValidName("prod_1", cluster.MaxNameLength))
	assert.False(t, cluster.ValidName("", cluster.MaxNameLength))
	assert.False(t, cluster.ValidName("a-very-long-node-pool-name-over-the-limit", cluster.MaxPoolNameLength))
}

func TestNewHostname_IsALabelUnderThePool(t *testing.T) {
	h := cluster.NewHostname("workers")
	assert.Regexp(t, `^workers-[0-9a-f]{6}$`, h)
	assert.True(t, cluster.ValidName(h, cluster.MaxNameLength))
}

func TestValidateLabels_FollowsKubernetesRules(t *testing.T) {
	assert.NoError(t, cluster.ValidateLabels(map[string]string{"tier": "gpu", "example.com/team": "a-b.c", "empty": ""}))
	assert.Error(t, cluster.ValidateLabels(map[string]string{"bad key!": "x"}))
	assert.Error(t, cluster.ValidateLabels(map[string]string{"tier": "has space"}))
	assert.Error(t, cluster.ValidateLabels(map[string]string{"Bad_Prefix/x": "y"}))
	assert.Error(t, cluster.ValidateLabels(map[string]string{"node-role.kubernetes.io/worker": ""}), "the kubelet refuses to start with it")
	assert.Error(t, cluster.ValidateLabels(map[string]string{"kubernetes.io/role": "web"}))
	assert.Error(t, cluster.ValidateLabels(map[string]string{"x.k8s.io/y": "z"}))
	assert.Error(t, cluster.ValidateLabels(map[string]string{"nodera.cloud/pool": "other"}), "Nodera names the pool")
	assert.NoError(t, cluster.ValidateLabels(map[string]string{"mykubernetes.io/tier": "web"}), "only the prefix itself or its subdomains are reserved")
	many := map[string]string{}
	for i := 0; i < cluster.MaxLabels+1; i++ {
		many[string(rune('a'+i))] = "x"
	}
	assert.Error(t, cluster.ValidateLabels(many))
}

func TestValidateTaints_FollowsKubernetesRules(t *testing.T) {
	assert.NoError(t, cluster.ValidateTaints([]cluster.Taint{{Key: "gpu", Value: "true", Effect: "NoSchedule"}, {Key: "batch", Effect: "PreferNoSchedule"}}))
	assert.Error(t, cluster.ValidateTaints([]cluster.Taint{{Key: "gpu", Value: "true", Effect: "Maybe"}}))
	assert.Error(t, cluster.ValidateTaints([]cluster.Taint{{Key: "", Effect: "NoExecute"}}))
}
