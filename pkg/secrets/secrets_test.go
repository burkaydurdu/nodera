package secrets_test

import (
	"bytes"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/piggystats/nodera/pkg/secrets"
)

func TestSealer_RoundTrips(t *testing.T) {
	sealer, err := secrets.NewSealer(bytes.Repeat([]byte{7}, 32))
	require.NoError(t, err)

	sealed, err := sealer.Seal([]byte("dkim private key"))
	require.NoError(t, err)
	assert.NotContains(t, string(sealed), "dkim private key")

	opened, err := sealer.Open(sealed)
	require.NoError(t, err)
	assert.Equal(t, "dkim private key", string(opened))
}

func TestSealer_RefusesAnotherKeysValue(t *testing.T) {
	a, _ := secrets.NewSealer(bytes.Repeat([]byte{1}, 32))
	b, _ := secrets.NewSealer(bytes.Repeat([]byte{2}, 32))

	sealed, err := a.Seal([]byte("secret"))
	require.NoError(t, err)

	_, err = b.Open(sealed)
	assert.Error(t, err)
}

func TestNewSealer_RefusesAShortKey(t *testing.T) {
	_, err := secrets.NewSealer([]byte("short"))
	assert.Error(t, err)
}

func TestDeriveFromBase64_IsStableAndApartPerPurpose(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))

	first, err := secrets.DeriveFromBase64(encoded, "return paths")
	require.NoError(t, err)
	again, err := secrets.DeriveFromBase64(encoded, "return paths")
	require.NoError(t, err)
	other, err := secrets.DeriveFromBase64(encoded, "another purpose")
	require.NoError(t, err)

	assert.Len(t, first, 32)
	assert.Equal(t, first, again)
	assert.NotEqual(t, first, other)
	assert.NotEqual(t, bytes.Repeat([]byte{7}, 32), first)
}

func TestDeriveFromBase64_RefusesAShortKey(t *testing.T) {
	_, err := secrets.DeriveFromBase64(base64.StdEncoding.EncodeToString([]byte("short")), "return paths")

	assert.Error(t, err)
}
