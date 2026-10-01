package droplet

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A throwaway ed25519 public key generated for this test; no private half exists.
const testEd25519Key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGpLYgdo0jRVvaW3cq4ZYq2C1a6D3kXAd0Y4r8p3gf1S test@nodera"

func TestParseSSHKeys_ComputesTheFingerprintItself(t *testing.T) {
	keys, err := ParseSSHKeys([]SSHKeyInput{{Name: "laptop", PublicKey: testEd25519Key}})
	require.NoError(t, err)
	require.Len(t, keys, 1)
	assert.Equal(t, "laptop", keys[0].Name)
	assert.Regexp(t, `^SHA256:[A-Za-z0-9+/]{43}$`, keys[0].Fingerprint)
}

func TestParseSSHKeys_RejectsGarbage(t *testing.T) {
	_, err := ParseSSHKeys([]SSHKeyInput{{Name: "bad", PublicKey: "not a key"}})
	assert.ErrorIs(t, err, ErrorInvalidSSHKey)
}

func TestParseSSHKeys_RejectsTheSameKeyTwice(t *testing.T) {
	_, err := ParseSSHKeys([]SSHKeyInput{
		{Name: "one", PublicKey: testEd25519Key},
		{Name: "two", PublicKey: testEd25519Key},
	})
	assert.ErrorIs(t, err, ErrorInvalidSSHKey)
}
