package apikey

import (
	"crypto/rand"
	"math/big"
	"strings"

	"github.com/piggystats/nodera/utils/hash"
)

// Marker begins every Nodera API key wherever it ends up, so a person or a secret
// scanner can tell it from any other token.
const Marker = "nd_"

const (
	idLength    = 12
	secretBytes = 32
	// secretLength is the most base62 digits 32 bytes need; a shorter encoding
	// is padded so every key has the same shape.
	secretLength = 43
	// base62Digits is math/big's digit order, so the id and the secret share
	// one alphabet.
	base62Digits = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
)

// NewKey draws a key's public id and its secret from crypto/rand.
func NewKey() (id, secret string, err error) {
	id, err = randomBase62(idLength)
	if err != nil {
		return "", "", ErrorCannotCreate.Copy().SetError(err)
	}

	raw := make([]byte, secretBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", "", ErrorCannotCreate.Copy().SetError(err)
	}
	encoded := new(big.Int).SetBytes(raw).Text(len(base62Digits))

	return id, strings.Repeat("0", secretLength-len(encoded)) + encoded, nil
}

// FullKey is the key as its creator copies it, once. A reader splits it at the
// dot, which base62 never contains.
func FullKey(id, secret string) string {
	return Marker + id + "." + secret
}

// HashSecret is what the row and the Redis entry keep. The id is public, so it
// stays out of the hash.
func HashSecret(secret string) string {
	return hash.SHA256Hex(secret)
}

// randomBase62 draws each character uniformly: a random byte modulo 62 would
// favour the first eight.
func randomBase62(length int) (string, error) {
	alphabet := big.NewInt(int64(len(base62Digits)))
	out := make([]byte, length)
	for i := range out {
		n, err := rand.Int(rand.Reader, alphabet)
		if err != nil {
			return "", err
		}
		out[i] = base62Digits[n.Int64()]
	}

	return string(out), nil
}
