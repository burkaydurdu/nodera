package hash

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"

	"github.com/google/uuid"
)

func GenerateUUID() string {
	return uuid.New().String()
}

// RandomHex returns n random bytes rendered as hex, the shape of every opaque
// token this service hands out (challenge, password reset).
func RandomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	return hex.EncodeToString(b), nil
}

func SHA256Hex(data string) string {
	sum := sha256.Sum256([]byte(data))

	return hex.EncodeToString(sum[:])
}

func EncodeHmacSHA256WithBase64(data, key string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(data))

	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// ConstantTimeEquals compares two short secrets (codes, tokens) without leaking
// where they diverge.
func ConstantTimeEquals(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
