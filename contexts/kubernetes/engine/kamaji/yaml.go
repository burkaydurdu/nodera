package kamaji

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

// The tokens an addon manifest may carry for the tenant's own API server, so
// a CNI that replaces kube-proxy knows where to reach it.
const (
	tokenAPIHost = "__NODERA_API_HOST__" //nolint:gosec // a placeholder in a manifest, not a secret
	tokenAPIPort = "__NODERA_API_PORT__" //nolint:gosec // a placeholder in a manifest, not a secret
)

// parseDocuments splits a multi-document YAML into objects; empty documents
// are skipped.
func parseDocuments(raw []byte) ([]map[string]any, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	var out []map[string]any
	for {
		var doc map[string]any
		err := decoder.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		if len(doc) == 0 {
			continue
		}
		if _, ok := doc["kind"].(string); !ok {
			return nil, errors.New("a document has no kind")
		}
		out = append(out, doc)
	}
}

// substitute writes the API server's host and port into every string of the
// object that names the tokens.
func substitute(object map[string]any, host, port string) map[string]any {
	replaced, _ := walk(object, func(s string) string {
		return strings.ReplaceAll(strings.ReplaceAll(s, tokenAPIHost, host), tokenAPIPort, port)
	}).(map[string]any)

	return replaced
}

func walk(value any, fn func(string) string) any {
	switch v := value.(type) {
	case string:
		return fn(v)
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, item := range v {
			out[k] = walk(item, fn)
		}

		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = walk(item, fn)
		}

		return out
	default:
		return v
	}
}

// adminKubeconfig is what the adapter needs of the kubeconfig Kamaji writes:
// where the API server is and the admin's certificate.
type adminKubeconfig struct {
	server  string
	caPEM   []byte
	certPEM []byte
	keyPEM  []byte
}

func parseKubeconfig(raw []byte) (*adminKubeconfig, error) {
	var kc struct {
		Clusters []struct {
			Cluster struct {
				Server string `yaml:"server"`
				CA     string `yaml:"certificate-authority-data"`
			} `yaml:"cluster"`
		} `yaml:"clusters"`
		Users []struct {
			User struct {
				Cert string `yaml:"client-certificate-data"`
				Key  string `yaml:"client-key-data"`
			} `yaml:"user"`
		} `yaml:"users"`
	}
	if err := yaml.Unmarshal(raw, &kc); err != nil {
		return nil, fmt.Errorf("kamaji: the admin kubeconfig: %w", err)
	}
	if len(kc.Clusters) == 0 || len(kc.Users) == 0 {
		return nil, errors.New("kamaji: the admin kubeconfig names no cluster or no user")
	}
	ca, err := base64.StdEncoding.DecodeString(kc.Clusters[0].Cluster.CA)
	if err != nil {
		return nil, err
	}
	cert, err := base64.StdEncoding.DecodeString(kc.Users[0].User.Cert)
	if err != nil {
		return nil, err
	}
	key, err := base64.StdEncoding.DecodeString(kc.Users[0].User.Key)
	if err != nil {
		return nil, err
	}

	return &adminKubeconfig{server: kc.Clusters[0].Cluster.Server, caPEM: ca, certPEM: cert, keyPEM: key}, nil
}

// kubeconfig writes the file a member downloads: one cluster, one user, one
// context.
func kubeconfig(name, server string, caPEM, certPEM, keyPEM []byte, user string) []byte {
	b64 := base64.StdEncoding.EncodeToString
	doc := map[string]any{
		"apiVersion":      "v1",
		"kind":            "Config",
		"clusters":        []map[string]any{{"name": name, "cluster": map[string]any{"server": server, "certificate-authority-data": b64(caPEM)}}},
		"users":           []map[string]any{{"name": user, "user": map[string]any{"client-certificate-data": b64(certPEM), "client-key-data": b64(keyPEM)}}},
		"contexts":        []map[string]any{{"name": name, "context": map[string]any{"cluster": name, "user": user}}},
		"current-context": name,
	}
	out, _ := yaml.Marshal(doc)

	return out
}
