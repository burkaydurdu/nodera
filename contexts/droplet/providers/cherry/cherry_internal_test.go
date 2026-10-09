package cherry

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/cherryservers/cherrygo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/piggystats/nodera/contexts/droplet/providers"
	"github.com/piggystats/nodera/logger"
)

// fakeKeys stands in for Cherry's key API: labels are unique across the
// account, as Cherry enforces them. beforeCreate lets a test slip another
// order's upload in between the lookup and the create.
type fakeKeys struct {
	cherrygo.SSHKeysService
	mu           sync.Mutex
	keys         []cherrygo.SSHKey
	beforeCreate func()
}

func (f *fakeKeys) List(context.Context, *cherrygo.GetOptions) ([]cherrygo.SSHKey, *cherrygo.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]cherrygo.SSHKey(nil), f.keys...), nil, nil
}

func (f *fakeKeys) Create(_ context.Context, request *cherrygo.CreateSSHKey) (cherrygo.SSHKey, *cherrygo.Response, error) {
	if f.beforeCreate != nil {
		f.beforeCreate()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, k := range f.keys {
		if k.Label == request.Label {
			return cherrygo.SSHKey{}, refused(), errors.New(`Parameter "label" violated a constraint "A key with this label already exists."`)
		}
	}
	key := cherrygo.SSHKey{ID: len(f.keys) + 100, Label: request.Label, Key: request.Key}
	f.keys = append(f.keys, key)

	return key, nil, nil
}

func (f *fakeKeys) add(label, key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keys = append(f.keys, cherrygo.SSHKey{ID: len(f.keys) + 100, Label: label, Key: key})
}

type fakeServers struct {
	cherrygo.ServersService
	got *cherrygo.CreateServer
}

func (f *fakeServers) Create(_ context.Context, request *cherrygo.CreateServer) (cherrygo.Server, *cherrygo.Response, error) {
	f.got = request

	return cherrygo.Server{ID: 7}, nil, nil
}

func refused() *cherrygo.Response {
	return &cherrygo.Response{Response: &http.Response{StatusCode: http.StatusBadRequest}}
}

func newTestCherry(keys *fakeKeys, servers *fakeServers) *Cherry {
	return &Cherry{api: api{sshKeys: keys, servers: servers}, projectID: 1, log: logger.Noop()}
}

const (
	laptopA = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGpLYgdo0jRVvaW3cq4ZYq2C1a6D3kXAd0Y4r8p3gf1S a@team-a"
	laptopB = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIC1XxvJ0m2aH4b7vK9fXfM0wq3Ycp8rZ4QbQyJ8oV2mN b@team-b"
)

func TestEnsureSSHKey_TwoKeysOfOneNameAreBothUploaded(t *testing.T) {
	keys := &fakeKeys{}
	c := newTestCherry(keys, nil)

	a, err := c.EnsureSSHKey(context.Background(), "laptop", laptopA)
	require.NoError(t, err)
	b, err := c.EnsureSSHKey(context.Background(), "laptop", laptopB)
	require.NoError(t, err, "every customer has a key called laptop")

	assert.NotEqual(t, a, b)
	require.Len(t, keys.keys, 2)
	assert.True(t, strings.HasPrefix(keys.keys[0].Label, "laptop-"))
	assert.NotEqual(t, keys.keys[0].Label, keys.keys[1].Label)
}

func TestEnsureSSHKey_AKnownKeyIsFoundWhateverItsName(t *testing.T) {
	keys := &fakeKeys{}
	keys.add("old name", laptopA)
	c := newTestCherry(keys, nil)

	ref, err := c.EnsureSSHKey(context.Background(), "laptop", strings.Replace(laptopA, "a@team-a", "renamed", 1))

	require.NoError(t, err)
	assert.Equal(t, strconv.Itoa(keys.keys[0].ID), ref, "matched on the key material, not the label or the comment")
	assert.Len(t, keys.keys, 1)
}

// Two Node orders upload the operational key at the same moment: both find
// nothing, the other one creates first, and this one takes the key it made.
func TestEnsureSSHKey_AnUploadThatLostTheRaceAnswersTheWinnersKey(t *testing.T) {
	keys := &fakeKeys{}
	keys.beforeCreate = func() {
		keys.beforeCreate = nil
		keys.add(keyLabel("nodera-ops", keyMaterial(laptopA)), laptopA)
	}
	c := newTestCherry(keys, nil)

	ref, err := c.EnsureSSHKey(context.Background(), "nodera-ops", laptopA)

	require.NoError(t, err)
	assert.Equal(t, strconv.Itoa(keys.keys[0].ID), ref)
	assert.Len(t, keys.keys, 1)
}

func TestEnsureSSHKey_ARefusalForAnotherReasonIsReported(t *testing.T) {
	keys := &fakeKeys{}
	keys.add(keyLabel("laptop", keyMaterial(laptopB)), "ssh-ed25519 something-else")
	c := newTestCherry(keys, nil)
	// A label taken by a different key, which no fingerprint should ever produce,
	// is still Cherry's no.
	_, err := c.EnsureSSHKey(context.Background(), "laptop", laptopB)

	assert.Error(t, err)
}

func TestKeyLabel_StaysWithinSixtyFourCharacters(t *testing.T) {
	label := keyLabel(strings.Repeat("k", 80), keyMaterial(laptopA))

	assert.Len(t, label, 64)
	assert.Equal(t, keyLabel(strings.Repeat("k", 80), keyMaterial(laptopA)), label, "one key, one label")
}

func TestCreateServer_SendsTheCloudInitAsBase64(t *testing.T) {
	servers := &fakeServers{}
	c := newTestCherry(&fakeKeys{}, servers)

	_, err := c.CreateServer(context.Background(), providers.ServerSpec{PlanRef: "p", RegionRef: "r", ImageRef: "i", Hostname: "h", UserData: "#cloud-config\nruncmd: [kubeadm join]"})

	require.NoError(t, err)
	decoded, err := base64.StdEncoding.DecodeString(servers.got.UserData)
	require.NoError(t, err)
	assert.Equal(t, "#cloud-config\nruncmd: [kubeadm join]", string(decoded))
	assert.Equal(t, "hourly", servers.got.Cycle)
}

func TestCreateServer_SendsNoUserDataForACustomersDroplet(t *testing.T) {
	servers := &fakeServers{}
	c := newTestCherry(&fakeKeys{}, servers)

	_, err := c.CreateServer(context.Background(), providers.ServerSpec{PlanRef: "p", RegionRef: "r", ImageRef: "i", Hostname: "h"})

	require.NoError(t, err)
	assert.Empty(t, servers.got.UserData)
}
