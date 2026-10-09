// Package kamaji is the engine adapter for Nodera's control plane fleet
// (docs/adr/0013): a management cluster running Kamaji, where each Cluster's
// Control plane is a TenantControlPlane. The adapter speaks the Kubernetes
// API over HTTP to the management cluster and, with the admin kubeconfig
// Kamaji writes, to each tenant's API server. It keeps Kamaji's vocabulary
// inside and answers in Nodera's.
package kamaji

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/kubernetes/engine"
	"github.com/piggystats/nodera/utils/str"
)

// Config is how the service reaches the management cluster and what every
// Control plane is made of.
type Config struct {
	APIServer     string
	Token         string
	CACertificate string
	Namespace     string
	DataStore     string
	Replicas      int
	AddonsDir     string
	Timeout       time.Duration
}

const (
	tcpAPI          = "/apis/kamaji.clastix.io/v1alpha1/namespaces/%s/tenantcontrolplanes"
	defaultReplicas = 2
	// sharingKey lets every Control plane's Service hold the Region's one
	// address, each on its own ports, where Cilium's LB IPAM hands it out.
	sharingKey      = "nodera-control-planes"
	fieldManager    = "nodera"
	clusterLabel    = "nodera.cloud/cluster-id"
	bootstrapGroups = "system:bootstrappers:kubeadm:default-node-token"
)

type Client struct {
	cfg    Config
	mgmt   *rest
	addons []map[string]any
}

// The adapter is the Engine the handlers consume; the assertion lives with
// the handlers' interface in the contract test.

func New(cfg Config) (*Client, error) {
	if str.IsBlank(cfg.APIServer) {
		return nil, errors.New("kamaji: the management cluster's api server is not configured")
	}
	if cfg.Replicas == 0 {
		cfg.Replicas = defaultReplicas
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	mgmt, err := newRest(cfg.APIServer, cfg.Timeout, func(t *tls.Config) error {
		if str.IsBlank(cfg.CACertificate) {
			return nil
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(cfg.CACertificate)) {
			return errors.New("kamaji: the management cluster's ca certificate is not pem")
		}
		t.RootCAs = pool

		return nil
	}, func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+cfg.Token) })
	if err != nil {
		return nil, err
	}

	addons, err := loadAddons(cfg.AddonsDir)
	if err != nil {
		return nil, err
	}

	return &Client{cfg: cfg, mgmt: mgmt, addons: addons}, nil
}

func (c *Client) tcpName(clusterID uuid.UUID) string { return "tcp-" + clusterID.String() }

func (c *Client) tcpPath(clusterID uuid.UUID) string {
	return fmt.Sprintf(tcpAPI, c.cfg.Namespace) + "/" + c.tcpName(clusterID)
}

// CreateControlPlane applies the TenantControlPlane; applying what already
// holds changes nothing.
func (c *Client) CreateControlPlane(ctx context.Context, spec engine.ControlPlaneSpec) error {
	return c.mgmt.apply(ctx, c.tcpPath(spec.ClusterID), c.tenantControlPlane(spec))
}

// tenantControlPlane is the Control plane as Kamaji takes it. Its API server
// answers on the Cluster's own port of the Region's address and its tunnel on
// the next, so a pod in the Cluster reaches it by address alone, where no
// host name is sent to route by (docs/adr/0015); the certificate also names
// the Endpoint's host, which Kubeconfigs dial.
func (c *Client) tenantControlPlane(spec engine.ControlPlaneSpec) map[string]any {
	return map[string]any{
		"apiVersion": "kamaji.clastix.io/v1alpha1",
		"kind":       "TenantControlPlane",
		"metadata": map[string]any{
			"name": c.tcpName(spec.ClusterID), "namespace": c.cfg.Namespace,
			"labels": map[string]string{clusterLabel: spec.ClusterID.String(), "nodera.cloud/region": spec.Region},
		},
		"spec": map[string]any{
			"dataStore": c.cfg.DataStore,
			"controlPlane": map[string]any{
				"deployment": map[string]any{"replicas": c.cfg.Replicas},
				"service": map[string]any{
					"serviceType": "LoadBalancer",
					// The management cluster's NodePort range would cap a
					// Region at a few thousand ports; the address needs none.
					"allocateLoadBalancerNodePorts": false,
					"additionalMetadata":            map[string]any{"annotations": map[string]string{"lbipam.cilium.io/sharing-key": sharingKey}},
				},
			},
			"kubernetes": map[string]any{
				"version": "v" + strings.TrimPrefix(spec.Version, "v"),
				"kubelet": map[string]any{"cgroupfs": "systemd"},
			},
			"networkProfile": map[string]any{"port": spec.Port, "certSANs": []string{spec.Hostname}},
			"addons": map[string]any{
				"coreDNS":      map[string]any{},
				"konnectivity": map[string]any{"server": map[string]any{"port": spec.TunnelPort}},
			},
		},
	}
}

// ControlPlaneStatus reads Kamaji's own word for the Control plane, then asks
// the API server itself, at its address and as the admin kubeconfig's
// identity: Kamaji calls it ready before its Service answers there and before
// it grants that identity cluster-admin, and ready must mean the service can
// prepare it and a Node can join. A refusal reads as pending, as it also does
// while Kamaji wipes a deleted Control plane's data under a running API server.
func (c *Client) ControlPlaneStatus(ctx context.Context, clusterID uuid.UUID) (*engine.ControlPlaneStatus, error) {
	status, body, err := c.mgmt.do(ctx, http.MethodGet, c.tcpPath(clusterID), nil, "")
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return &engine.ControlPlaneStatus{State: engine.ControlPlaneMissing}, nil
	}
	var tcp struct {
		Spec struct {
			Kubernetes struct {
				Version string `json:"version"`
			} `json:"kubernetes"`
		} `json:"spec"`
		Status struct {
			KubernetesResources struct {
				Version struct {
					Status string `json:"status"`
				} `json:"version"`
			} `json:"kubernetesResources"`
		} `json:"status"`
	}
	if err := json.Unmarshal(body, &tcp); err != nil {
		return nil, err
	}
	out := &engine.ControlPlaneStatus{State: engine.ControlPlanePending, Version: strings.TrimPrefix(tcp.Spec.Kubernetes.Version, "v")}
	if tcp.Status.KubernetesResources.Version.Status != "Ready" {
		return out, nil
	}
	t, err := c.tenant(ctx, clusterID)
	if err != nil {
		if errors.Is(err, engine.ErrNotFound) {
			return out, nil
		}

		return nil, err
	}
	answered, _, err := t.rest.do(ctx, http.MethodGet, "/api/v1/namespaces/kube-system", nil, "")
	switch {
	case errors.Is(err, engine.ErrUnavailable), answered == http.StatusUnauthorized, answered == http.StatusForbidden:
		return out, nil
	case err != nil:
		return nil, err
	case answered != http.StatusOK:
		return out, nil
	}
	out.State = engine.ControlPlaneReady

	return out, nil
}

func (c *Client) DeleteControlPlane(ctx context.Context, clusterID uuid.UUID) error {
	status, _, err := c.mgmt.do(ctx, http.MethodDelete, c.tcpPath(clusterID), nil, "")
	if err != nil {
		return err
	}
	if status == http.StatusNotFound {
		return nil
	}

	return nil
}

// PrepareCluster applies the addons into the tenant (the CNI, with the API
// server's address written in) and binds the Kubeconfigs' group to
// cluster-admin. Applying twice changes nothing.
func (c *Client) PrepareCluster(ctx context.Context, clusterID uuid.UUID) error {
	t, err := c.tenant(ctx, clusterID)
	if err != nil {
		return err
	}
	binding := map[string]any{
		"apiVersion": "rbac.authorization.k8s.io/v1",
		"kind":       "ClusterRoleBinding",
		"metadata":   map[string]any{"name": "nodera-cluster-admins"},
		"roleRef":    map[string]any{"apiGroup": "rbac.authorization.k8s.io", "kind": "ClusterRole", "name": "cluster-admin"},
		"subjects":   []map[string]any{{"apiGroup": "rbac.authorization.k8s.io", "kind": "Group", "name": engine.AdminsGroup}},
	}
	if err := t.rest.apply(ctx, resourcePath(binding), binding); err != nil {
		return err
	}
	for _, doc := range c.addons {
		rendered := substitute(doc, t.host, t.port)
		if err := t.rest.apply(ctx, resourcePath(rendered), rendered); err != nil {
			return err
		}
	}

	return nil
}

// CreateJoin makes a kubeadm bootstrap token in the tenant, good for ttl, and
// answers it with the pin of the tenant's CA.
func (c *Client) CreateJoin(ctx context.Context, clusterID uuid.UUID, ttl time.Duration) (*engine.Join, error) {
	t, err := c.tenant(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	id, secret := randomToken(6), randomToken(16)
	expires := time.Now().UTC().Add(ttl)
	bootstrap := map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"type":       "bootstrap.kubernetes.io/token",
		"metadata":   map[string]any{"name": "bootstrap-token-" + id, "namespace": "kube-system"},
		"stringData": map[string]string{
			"token-id": id, "token-secret": secret, "expiration": expires.Format(time.RFC3339),
			"usage-bootstrap-authentication": "true", "usage-bootstrap-signing": "true",
			"auth-extra-groups": bootstrapGroups, "description": "nodera node join",
		},
	}
	if err := t.rest.apply(ctx, resourcePath(bootstrap), bootstrap); err != nil {
		return nil, err
	}

	return &engine.Join{
		Endpoint:   net.JoinHostPort(t.host, t.port),
		Token:      id + "." + secret,
		CACertHash: caHash(t.ca),
		ExpiresAt:  expires,
	}, nil
}

// IssueKubeconfig signs a client certificate with the tenant's CA, which
// Kamaji keeps in the management cluster, and wraps it in a kubeconfig.
func (c *Client) IssueKubeconfig(ctx context.Context, clusterID uuid.UUID, spec engine.KubeconfigSpec) ([]byte, error) {
	t, err := c.tenant(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	caCert, caKey, err := c.tenantCA(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	certPEM, keyPEM, err := signClient(caCert, caKey, spec)
	if err != nil {
		return nil, err
	}

	server := spec.Server
	if str.IsBlank(server) {
		server = "https://" + net.JoinHostPort(t.host, t.port)
	}

	return kubeconfig(clusterID.String(), server, t.caPEM, certPEM, keyPEM, spec.User), nil
}

func (c *Client) ListNodes(ctx context.Context, clusterID uuid.UUID) ([]engine.Node, error) {
	t, err := c.tenant(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	status, body, err := t.rest.do(ctx, http.MethodGet, "/api/v1/nodes", nil, "")
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("kamaji: listing nodes answered %d", status)
	}
	var list struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Spec struct {
				ProviderID string `json:"providerID"`
			} `json:"spec"`
			Status struct {
				Conditions []struct {
					Type   string `json:"type"`
					Status string `json:"status"`
				} `json:"conditions"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, err
	}
	out := make([]engine.Node, 0, len(list.Items))
	for _, item := range list.Items {
		ready := false
		for _, cond := range item.Status.Conditions {
			if cond.Type == "Ready" && cond.Status == "True" {
				ready = true
			}
		}
		out = append(out, engine.Node{Name: item.Metadata.Name, ProviderID: item.Spec.ProviderID, Ready: ready})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	return out, nil
}

// DrainNode cordons the Node and evicts every pod on it that is not a
// DaemonSet's or a static one; forced, it deletes them at once. It answers
// ErrDraining while any such pod was still there, so the caller asks again.
func (c *Client) DrainNode(ctx context.Context, clusterID uuid.UUID, name string, force bool) error {
	t, err := c.tenant(ctx, clusterID)
	if err != nil {
		return err
	}
	status, _, err := t.rest.do(ctx, http.MethodPatch, "/api/v1/nodes/"+name, []byte(`{"spec":{"unschedulable":true}}`), "application/merge-patch+json")
	if err != nil {
		return err
	}
	if status == http.StatusNotFound {
		return engine.ErrNotFound
	}

	status, body, err := t.rest.do(ctx, http.MethodGet, "/api/v1/pods?fieldSelector=spec.nodeName%3D"+name, nil, "")
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("kamaji: listing pods answered %d", status)
	}
	var pods struct {
		Items []struct {
			Metadata struct {
				Name            string            `json:"name"`
				Namespace       string            `json:"namespace"`
				Annotations     map[string]string `json:"annotations"`
				OwnerReferences []struct {
					Kind string `json:"kind"`
				} `json:"ownerReferences"`
			} `json:"metadata"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &pods); err != nil {
		return err
	}

	remaining := 0
	for _, pod := range pods.Items {
		if _, mirror := pod.Metadata.Annotations["kubernetes.io/config.mirror"]; mirror {
			continue
		}
		daemon := false
		for _, owner := range pod.Metadata.OwnerReferences {
			if owner.Kind == "DaemonSet" {
				daemon = true
			}
		}
		if daemon {
			continue
		}
		remaining++
		path := fmt.Sprintf("/api/v1/namespaces/%s/pods/%s", pod.Metadata.Namespace, pod.Metadata.Name)
		if force {
			_, _, err = t.rest.do(ctx, http.MethodDelete, path+"?gracePeriodSeconds=0", nil, "")
		} else {
			eviction := fmt.Sprintf(`{"apiVersion":"policy/v1","kind":"Eviction","metadata":{"name":%q,"namespace":%q}}`, pod.Metadata.Name, pod.Metadata.Namespace)
			_, _, err = t.rest.do(ctx, http.MethodPost, path+"/eviction", []byte(eviction), "application/json")
		}
		if err != nil {
			return err
		}
	}
	if remaining > 0 {
		return engine.ErrDraining
	}

	return nil
}

func (c *Client) DeleteNode(ctx context.Context, clusterID uuid.UUID, name string) error {
	t, err := c.tenant(ctx, clusterID)
	if err != nil {
		return err
	}
	status, _, err := t.rest.do(ctx, http.MethodDelete, "/api/v1/nodes/"+name, nil, "")
	if err != nil {
		return err
	}
	if status == http.StatusNotFound {
		return engine.ErrNotFound
	}

	return nil
}

// ---------------------------------------------------------------------------
// Tenants
// ---------------------------------------------------------------------------

// tenant is one Cluster's API server as the admin kubeconfig Kamaji wrote
// reaches it: the address it advertises to the Nodes, with its own port.
type tenant struct {
	rest  *rest
	host  string
	port  string
	ca    *x509.Certificate
	caPEM []byte
}

func (c *Client) tenant(ctx context.Context, clusterID uuid.UUID) (*tenant, error) {
	data, err := c.secret(ctx, c.tcpName(clusterID)+"-admin-kubeconfig")
	if err != nil {
		return nil, err
	}
	raw, ok := data["admin.conf"]
	if !ok {
		return nil, engine.ErrNotFound
	}
	kc, err := parseKubeconfig(raw)
	if err != nil {
		return nil, err
	}
	ca, err := parseCertificate(kc.caPEM)
	if err != nil {
		return nil, err
	}
	clientCert, err := tls.X509KeyPair(kc.certPEM, kc.keyPEM)
	if err != nil {
		return nil, fmt.Errorf("kamaji: the admin kubeconfig's client certificate: %w", err)
	}
	r, err := newRest(kc.server, c.cfg.Timeout, func(t *tls.Config) error {
		pool := x509.NewCertPool()
		pool.AddCert(ca)
		t.RootCAs = pool
		t.Certificates = []tls.Certificate{clientCert}

		return nil
	}, nil)
	if err != nil {
		return nil, err
	}
	host, port, err := splitServer(kc.server)
	if err != nil {
		return nil, err
	}

	return &tenant{rest: r, host: host, port: port, ca: ca, caPEM: kc.caPEM}, nil
}

// splitServer answers the host and the port of a kubeconfig's server; every
// Control plane has a port of its own, so one without it is not one of ours.
func splitServer(server string) (string, string, error) {
	host, port, err := net.SplitHostPort(strings.TrimPrefix(server, "https://"))
	if err != nil {
		return "", "", fmt.Errorf("kamaji: the admin kubeconfig's server %q: %w", server, err)
	}

	return host, port, nil
}

func (c *Client) tenantCA(ctx context.Context, clusterID uuid.UUID) (*x509.Certificate, any, error) {
	data, err := c.secret(ctx, c.tcpName(clusterID)+"-ca")
	if err != nil {
		return nil, nil, err
	}
	cert, err := parseCertificate(data["ca.crt"])
	if err != nil {
		return nil, nil, err
	}
	key, err := parsePrivateKey(data["ca.key"])
	if err != nil {
		return nil, nil, err
	}

	return cert, key, nil
}

// secret reads one Secret of the management cluster's Namespace, decoded.
func (c *Client) secret(ctx context.Context, name string) (map[string][]byte, error) {
	status, body, err := c.mgmt.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/namespaces/%s/secrets/%s", c.cfg.Namespace, name), nil, "")
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, engine.ErrNotFound
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("kamaji: reading secret %s answered %d", name, status)
	}
	var s struct {
		Data map[string][]byte `json:"data"`
	}
	if err := json.Unmarshal(body, &s); err != nil {
		return nil, err
	}

	return s.Data, nil
}

// ---------------------------------------------------------------------------
// HTTP
// ---------------------------------------------------------------------------

// rest is one API server reached over HTTP: the management cluster with a
// bearer token, a tenant with its client certificate.
type rest struct {
	base   string
	client *http.Client
	auth   func(*http.Request)
}

func newRest(base string, timeout time.Duration, tlsSetup func(*tls.Config) error, auth func(*http.Request)) (*rest, error) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if err := tlsSetup(tlsConfig); err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsConfig

	return &rest{base: strings.TrimRight(base, "/"), client: &http.Client{Timeout: timeout, Transport: transport}, auth: auth}, nil
}

// do answers the status and the body; a server that could not be reached is
// engine.ErrUnavailable, a 5xx answer too. A 4xx other than 404 is an error
// of its own, since repeating it will not help.
func (r *rest) do(ctx context.Context, method, path string, body []byte, contentType string) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, r.base+path, reader)
	if err != nil {
		return 0, nil, err
	}
	if str.IsNotEmpty(contentType) {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")
	if r.auth != nil {
		r.auth(req)
	}
	res, err := r.client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: %v", engine.ErrUnavailable, err)
	}
	defer func() { _ = res.Body.Close() }()
	out, err := io.ReadAll(res.Body)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: %v", engine.ErrUnavailable, err)
	}
	switch {
	case res.StatusCode >= 500:
		return res.StatusCode, out, fmt.Errorf("%w: %s %s answered %d", engine.ErrUnavailable, method, path, res.StatusCode)
	case res.StatusCode == http.StatusUnauthorized, res.StatusCode == http.StatusForbidden:
		return res.StatusCode, out, fmt.Errorf("kamaji: %s %s refused with %d", method, path, res.StatusCode)
	case res.StatusCode >= 400 && res.StatusCode != http.StatusNotFound:
		return res.StatusCode, out, fmt.Errorf("kamaji: %s %s answered %d: %s", method, path, res.StatusCode, strings.TrimSpace(string(out)))
	}

	return res.StatusCode, out, nil
}

// apply is a server-side apply of one object as Nodera's field manager; it
// creates or updates and is idempotent.
func (r *rest) apply(ctx context.Context, path string, object map[string]any) error {
	body, err := json.Marshal(object)
	if err != nil {
		return err
	}
	_, _, err = r.do(ctx, http.MethodPatch, path+"?fieldManager="+fieldManager+"&force=true", body, "application/apply-patch+yaml")

	return err
}

// resourcePath is where an object lives in its API server, from its
// apiVersion, kind and metadata.
func resourcePath(object map[string]any) string {
	apiVersion, _ := object["apiVersion"].(string)
	kind, _ := object["kind"].(string)
	metadata, _ := object["metadata"].(map[string]any)
	name, _ := metadata["name"].(string)
	namespace, _ := metadata["namespace"].(string)

	prefix := "/apis/" + apiVersion
	if apiVersion == "v1" {
		prefix = "/api/v1"
	}
	if str.IsNotEmpty(namespace) {
		prefix += "/namespaces/" + namespace
	}

	return prefix + "/" + plural(kind) + "/" + name
}

// plural is the resource name of a kind; the irregular ones are listed, the
// rest are the lower-cased kind with an s.
func plural(kind string) string {
	switch kind {
	case "Ingress":
		return "ingresses"
	case "NetworkPolicy":
		return "networkpolicies"
	case "PodSecurityPolicy":
		return "podsecuritypolicies"
	case "Endpoints":
		return "endpoints"
	case "CiliumClusterwideNetworkPolicy":
		return "ciliumclusterwidenetworkpolicies"
	case "CiliumNetworkPolicy":
		return "ciliumnetworkpolicies"
	default:
		return strings.ToLower(kind) + "s"
	}
}

// loadAddons reads every YAML document under dir, in file name order.
func loadAddons(dir string) ([]map[string]any, error) {
	if str.IsBlank(dir) {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}

		return nil, err
	}
	var docs []map[string]any
	for _, entry := range entries {
		if entry.IsDir() || (!strings.HasSuffix(entry.Name(), ".yaml") && !strings.HasSuffix(entry.Name(), ".yml")) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name())) // #nosec G304 -- the directory is the operator's configuration
		if err != nil {
			return nil, err
		}
		parsed, err := parseDocuments(raw)
		if err != nil {
			return nil, fmt.Errorf("kamaji: addon %s: %w", entry.Name(), err)
		}
		docs = append(docs, parsed...)
	}

	return docs, nil
}
