package kamaji

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/piggystats/nodera/contexts/kubernetes/engine"
)

func TestResourcePath_FollowsTheObjectsAPIVersionKindAndScope(t *testing.T) {
	assert.Equal(t, "/api/v1/namespaces/kube-system/secrets/bootstrap-token-abc",
		resourcePath(map[string]any{"apiVersion": "v1", "kind": "Secret", "metadata": map[string]any{"name": "bootstrap-token-abc", "namespace": "kube-system"}}))
	assert.Equal(t, "/apis/rbac.authorization.k8s.io/v1/clusterrolebindings/nodera-cluster-admins",
		resourcePath(map[string]any{"apiVersion": "rbac.authorization.k8s.io/v1", "kind": "ClusterRoleBinding", "metadata": map[string]any{"name": "nodera-cluster-admins"}}))
	assert.Equal(t, "/apis/networking.k8s.io/v1/namespaces/default/ingresses/web",
		resourcePath(map[string]any{"apiVersion": "networking.k8s.io/v1", "kind": "Ingress", "metadata": map[string]any{"name": "web", "namespace": "default"}}))
	assert.Equal(t, "/apis/cilium.io/v2/ciliumnetworkpolicies/x",
		resourcePath(map[string]any{"apiVersion": "cilium.io/v2", "kind": "CiliumNetworkPolicy", "metadata": map[string]any{"name": "x"}}))
}

func TestParseDocuments_SplitsAndSkipsEmptyOnes(t *testing.T) {
	docs, err := parseDocuments([]byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n---\n\n---\napiVersion: apps/v1\nkind: DaemonSet\nmetadata:\n  name: b\n  namespace: kube-system\n"))
	require.NoError(t, err)
	require.Len(t, docs, 2)
	assert.Equal(t, "ConfigMap", docs[0]["kind"])
	assert.Equal(t, "DaemonSet", docs[1]["kind"])

	_, err = parseDocuments([]byte("metadata:\n  name: no-kind\n"))
	assert.Error(t, err)
}

func TestSubstitute_WritesTheEndpointIntoEveryString(t *testing.T) {
	doc := map[string]any{
		"kind": "ConfigMap",
		"data": map[string]any{"k8s-service-host": tokenAPIHost, "k8s-service-port": tokenAPIPort, "list": []any{"x", tokenAPIHost + ":" + tokenAPIPort}},
	}

	out := substitute(doc, "c1.vno1.k8s.nodera.test", "6443")

	data := out["data"].(map[string]any)
	assert.Equal(t, "c1.vno1.k8s.nodera.test", data["k8s-service-host"])
	assert.Equal(t, "6443", data["k8s-service-port"])
	assert.Equal(t, "c1.vno1.k8s.nodera.test:6443", data["list"].([]any)[1])
	assert.Equal(t, tokenAPIHost, doc["data"].(map[string]any)["k8s-service-host"], "the original is left alone")
}

func TestKubeconfig_RoundTripsThroughTheParser(t *testing.T) {
	raw := kubeconfig("c1", "https://c1.vno1.k8s.nodera.test:6443", []byte("CA"), []byte("CERT"), []byte("KEY"), "member-1")

	kc, err := parseKubeconfig(raw)
	require.NoError(t, err)
	assert.Equal(t, "https://c1.vno1.k8s.nodera.test:6443", kc.server)
	assert.Equal(t, []byte("CA"), kc.caPEM)
	assert.Equal(t, []byte("CERT"), kc.certPEM)
	assert.Equal(t, []byte("KEY"), kc.keyPEM)
	assert.Contains(t, string(raw), "current-context: c1")
}

func TestRandomToken_IsLowerCaseAlphanumericOfTheLength(t *testing.T) {
	assert.Regexp(t, `^[a-z0-9]{6}$`, randomToken(6))
	assert.Regexp(t, `^[a-z0-9]{16}$`, randomToken(16))
	assert.NotEqual(t, randomToken(16), randomToken(16))
}

// A Control plane answers on its own port of the Region's address, behind a
// LoadBalancer Service that shares that address with every other Control
// plane and takes no NodePort of the management cluster's narrow range; the
// certificate names the Endpoint's host (docs/adr/0015).
func TestTenantControlPlane_AnswersOnItsOwnPortsOfTheSharedAddress(t *testing.T) {
	c := &Client{cfg: Config{Namespace: "nodera-vno1", DataStore: "default", Replicas: 2}}
	clusterID := uuid.MustParse("019a0000-0000-7000-8000-000000000001")
	spec := engine.ControlPlaneSpec{ClusterID: clusterID, Region: "vno1", Version: "1.37.0", Hostname: "c1.vno1.k8s.nodera.test", Port: 20004, TunnelPort: 20005}

	tcp := c.tenantControlPlane(spec)

	raw, err := json.Marshal(tcp)
	require.NoError(t, err)
	var got struct {
		Spec struct {
			ControlPlane struct {
				Gateway *struct{} `json:"gateway"`
				Service struct {
					ServiceType                   string `json:"serviceType"`
					AllocateLoadBalancerNodePorts *bool  `json:"allocateLoadBalancerNodePorts"`
					AdditionalMetadata            struct {
						Annotations map[string]string `json:"annotations"`
					} `json:"additionalMetadata"`
				} `json:"service"`
			} `json:"controlPlane"`
			NetworkProfile struct {
				Port     int      `json:"port"`
				CertSANs []string `json:"certSANs"`
			} `json:"networkProfile"`
			Addons struct {
				Konnectivity struct {
					Server struct {
						Port int `json:"port"`
					} `json:"server"`
				} `json:"konnectivity"`
			} `json:"addons"`
		} `json:"spec"`
	}
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Nil(t, got.Spec.ControlPlane.Gateway, "no host-name routing in front of it")
	assert.Equal(t, "LoadBalancer", got.Spec.ControlPlane.Service.ServiceType)
	require.NotNil(t, got.Spec.ControlPlane.Service.AllocateLoadBalancerNodePorts)
	assert.False(t, *got.Spec.ControlPlane.Service.AllocateLoadBalancerNodePorts)
	assert.Equal(t, "nodera-control-planes", got.Spec.ControlPlane.Service.AdditionalMetadata.Annotations["lbipam.cilium.io/sharing-key"])
	assert.Equal(t, 20004, got.Spec.NetworkProfile.Port)
	assert.Equal(t, []string{"c1.vno1.k8s.nodera.test"}, got.Spec.NetworkProfile.CertSANs)
	assert.Equal(t, 20005, got.Spec.Addons.Konnectivity.Server.Port)
}

// The tenant is reached at the address Kamaji advertises in its admin
// kubeconfig, which carries the Control plane's own port.
func TestSplitServer_AnswersTheHostAndThePort(t *testing.T) {
	host, port, err := splitServer("https://203.0.113.10:20004")
	require.NoError(t, err)
	assert.Equal(t, "203.0.113.10", host)
	assert.Equal(t, "20004", port)

	_, _, err = splitServer("https://203.0.113.10")
	assert.Error(t, err, "an address without its port is refused")
}
