package publickey

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	"github.com/piggystats/nodera/contexts/customer/aggregates/sshkey"
)

func ed25519Line(t *testing.T, comment string) (string, ssh.PublicKey) {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	sshPub, err := ssh.NewPublicKey(pub)
	require.NoError(t, err)
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
	if comment != "" {
		line += " " + comment
	}

	return line, sshPub
}

func rsaLine(t *testing.T, bits int) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, bits)
	require.NoError(t, err)
	sshPub, err := ssh.NewPublicKey(&key.PublicKey)
	require.NoError(t, err)

	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
}

func TestParse_Ed25519_KeepsCommentAndFingerprintsLikeSSHKeygen(t *testing.T) {
	line, sshPub := ed25519Line(t, "ada@laptop")

	parsed, err := Parse("  " + line + "\n")

	require.NoError(t, err)
	assert.Equal(t, line, parsed.PublicKey)
	assert.Equal(t, ssh.FingerprintSHA256(sshPub), parsed.Fingerprint)
	assert.True(t, strings.HasPrefix(parsed.Fingerprint, "SHA256:"))
	assert.False(t, strings.HasSuffix(parsed.Fingerprint, "="), "unpadded base64")
}

func TestParse_RSA2048_IsAccepted(t *testing.T) {
	line := rsaLine(t, 2048)

	parsed, err := Parse(line)

	require.NoError(t, err)
	assert.Equal(t, line, parsed.PublicKey)
}

func TestParse_RSA1024_IsTooShort(t *testing.T) {
	_, err := Parse(rsaLine(t, 1024))

	assert.True(t, errors.Is(err, sshkey.ErrorKeyTooShort), err)
}

func TestParse_ECDSA_IsUnsupported(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	sshPub, err := ssh.NewPublicKey(&key.PublicKey)
	require.NoError(t, err)

	_, err = Parse(string(ssh.MarshalAuthorizedKey(sshPub)))

	assert.True(t, errors.Is(err, sshkey.ErrorUnsupportedKeyType), err)
}

func TestParse_Garbage_IsInvalid(t *testing.T) {
	_, err := Parse("not a key at all")

	assert.True(t, errors.Is(err, sshkey.ErrorInvalidPublicKey), err)
}

func TestParse_TwoKeysOnOneLine_IsInvalid(t *testing.T) {
	first, _ := ed25519Line(t, "")
	second, _ := ed25519Line(t, "")

	_, err := Parse(first + "\n" + second)

	assert.True(t, errors.Is(err, sshkey.ErrorInvalidPublicKey), err)
}
