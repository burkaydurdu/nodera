package apikey_test

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/piggystats/nodera/contexts/customer/aggregates/apikey"
)

var (
	idShape     = regexp.MustCompile(`^[0-9A-Za-z]{12}$`)
	secretShape = regexp.MustCompile(`^[0-9A-Za-z]{43}$`)
	keyShape    = regexp.MustCompile(`^nd_[0-9A-Za-z]{12}\.[0-9A-Za-z]{43}$`)
)

func TestNewKey_IDIsTwelveBase62Characters(t *testing.T) {
	id, _, err := apikey.NewKey()

	require.NoError(t, err)
	assert.Regexp(t, idShape, id)
}

func TestNewKey_SecretIs32BytesInBase62WithAFixedLength(t *testing.T) {
	for range 200 {
		_, secret, err := apikey.NewKey()

		require.NoError(t, err)
		assert.Regexp(t, secretShape, secret)
	}
}

func TestNewKey_NeverRepeats(t *testing.T) {
	firstID, firstSecret, err := apikey.NewKey()
	require.NoError(t, err)
	secondID, secondSecret, err := apikey.NewKey()
	require.NoError(t, err)

	assert.NotEqual(t, firstID, secondID)
	assert.NotEqual(t, firstSecret, secondSecret)
}

func TestFullKey_IsThePrefixTheIDAndTheSecret(t *testing.T) {
	id, secret, err := apikey.NewKey()
	require.NoError(t, err)

	full := apikey.FullKey(id, secret)

	assert.Regexp(t, keyShape, full)
	assert.Equal(t, "nd_"+id+"."+secret, full)
}

func TestPrefix_IsWhatAListShows(t *testing.T) {
	key := &apikey.APIKey{ID: "AbCdEf012345"}

	assert.Equal(t, "nd_AbCdEf012345", key.Prefix())
}

func TestHashSecret_IsTheLowercaseSHA256HexOfTheSecretAlone(t *testing.T) {
	sum := sha256.Sum256([]byte("s3cr3t"))

	assert.Equal(t, hex.EncodeToString(sum[:]), apikey.HashSecret("s3cr3t"))
}
