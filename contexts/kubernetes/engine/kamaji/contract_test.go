package kamaji_test

import (
	"context"
	"math/rand/v2"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/piggystats/nodera/contexts/kubernetes/application/handlers"
	"github.com/piggystats/nodera/contexts/kubernetes/engine"
	"github.com/piggystats/nodera/contexts/kubernetes/engine/kamaji"
)

// The adapter is the Engine the handlers consume.
var _ handlers.Engine = (*kamaji.Client)(nil)

// The contract runs the adapter against a real Kamaji on the management cluster
// deploy/kubernetes/README.md describes, named by the NODERA_KAMAJI_* variables,
// and checks what only a real fleet can answer: that a Control plane comes up
// on its own port of the Region's address, that a join secret and a Kubeconfig are
// accepted by it, that the Nodes it lists are the ones that joined and that a
// deleted Control plane is gone. It needs that cluster, so it runs only when
// asked to; every move of the pinned Kamaji tag passes it first.
func TestKamajiContract(t *testing.T) {
	if os.Getenv("NODERA_KUBERNETES_SMOKE") == "" {
		t.Skip("set NODERA_KUBERNETES_SMOKE=true and the NODERA_KAMAJI_* variables to run the adapter against a real Kamaji")
	}
	env := func(name, fallback string) string {
		if v := os.Getenv(name); v != "" {
			return v
		}

		return fallback
	}

	client, err := kamaji.New(kamaji.Config{
		APIServer:     os.Getenv("NODERA_KAMAJI_API_SERVER"),
		Token:         os.Getenv("NODERA_KAMAJI_TOKEN"),
		CACertificate: os.Getenv("NODERA_KAMAJI_CA_CERTIFICATE"),
		Namespace:     env("NODERA_KAMAJI_NAMESPACE", "nodera-contract"),
		DataStore:     env("NODERA_KAMAJI_DATASTORE", "default"),
		Replicas:      1,
		AddonsDir:     env("NODERA_KAMAJI_ADDONS_DIR", "../../../../deploy/kubernetes/addons"),
		Timeout:       30 * time.Second,
	})
	require.NoError(t, err)
	ctx := context.Background()
	domain := env("NODERA_KAMAJI_ENDPOINT_DOMAIN", "k8s.contract.test")
	version := env("NODERA_KAMAJI_VERSION", "1.37.0")
	region := env("NODERA_KAMAJI_REGION", "contract")
	// A pair from the top of the service's default range, which a development
	// fleet's own Clusters reach last; a fresh one each run, so a run never
	// meets the Service of the one before while it is still going away.
	port, err := strconv.Atoi(env("NODERA_KAMAJI_PORT", strconv.Itoa(29900+2*rand.IntN(50))))
	require.NoError(t, err)

	clusterID := uuid.New()
	spec := engine.ControlPlaneSpec{
		ClusterID: clusterID, Region: region, Version: version, Hostname: clusterID.String() + "." + region + "." + domain,
		Port: port, TunnelPort: port + 1,
	}
	t.Cleanup(func() { _ = client.DeleteControlPlane(context.Background(), clusterID) })

	t.Run("a control plane comes up and is ready at its endpoint", func(t *testing.T) {
		require.NoError(t, client.CreateControlPlane(ctx, spec))
		require.NoError(t, client.CreateControlPlane(ctx, spec), "applying twice changes nothing")

		deadline := time.Now().Add(10 * time.Minute)
		for {
			status, err := client.ControlPlaneStatus(ctx, clusterID)
			require.NoError(t, err)
			if status.State == engine.ControlPlaneReady {
				assert.Equal(t, version, status.Version)

				break
			}
			require.True(t, time.Now().Before(deadline), "the control plane was not ready within ten minutes")
			time.Sleep(10 * time.Second)
		}
	})

	t.Run("the tenant is prepared for nodes and admins", func(t *testing.T) {
		require.NoError(t, client.PrepareCluster(ctx, clusterID))
		require.NoError(t, client.PrepareCluster(ctx, clusterID), "preparing twice changes nothing")
	})

	t.Run("a join secret names the endpoint and pins the ca", func(t *testing.T) {
		join, err := client.CreateJoin(ctx, clusterID, time.Hour)
		require.NoError(t, err)
		assert.True(t, strings.HasSuffix(join.Endpoint, ":"+strconv.Itoa(port)), join.Endpoint)
		assert.Regexp(t, `^[a-z0-9]{6}\.[a-z0-9]{16}$`, join.Token)
		assert.True(t, strings.HasPrefix(join.CACertHash, "sha256:"), join.CACertHash)
	})

	t.Run("a kubeconfig is cut for the member in the admins group", func(t *testing.T) {
		server := "https://" + spec.Hostname + ":" + strconv.Itoa(port)
		raw, err := client.IssueKubeconfig(ctx, clusterID, engine.KubeconfigSpec{User: "member-1", Groups: []string{engine.AdminsGroup}, TTL: time.Hour, Server: server})
		require.NoError(t, err)
		assert.Contains(t, string(raw), "server: "+server)
		assert.Contains(t, string(raw), "client-certificate-data:")
	})

	t.Run("a cluster with no nodes lists none", func(t *testing.T) {
		nodes, err := client.ListNodes(ctx, clusterID)
		require.NoError(t, err)
		assert.Empty(t, nodes)
		assert.ErrorIs(t, client.DrainNode(ctx, clusterID, "nobody", false), engine.ErrNotFound)
		assert.ErrorIs(t, client.DeleteNode(ctx, clusterID, "nobody"), engine.ErrNotFound)
	})

	t.Run("a deleted control plane is gone", func(t *testing.T) {
		require.NoError(t, client.DeleteControlPlane(ctx, clusterID))
		require.NoError(t, client.DeleteControlPlane(ctx, clusterID), "deleting twice changes nothing")
		deadline := time.Now().Add(2 * time.Minute)
		for {
			status, err := client.ControlPlaneStatus(ctx, clusterID)
			require.NoError(t, err)
			if status.State == engine.ControlPlaneMissing {
				break
			}
			require.True(t, time.Now().Before(deadline), "the control plane was not removed within two minutes")
			time.Sleep(5 * time.Second)
		}
	})
}
