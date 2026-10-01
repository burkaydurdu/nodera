package hash

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

type argonConfig struct {
	Time    uint32
	Memory  uint32
	Threads uint8
	KeyLen  uint32
}

var defaultArgonConfig = argonConfig{
	Time:    1,
	Memory:  64 * 1024,
	Threads: 4,
	KeyLen:  32,
}

const argonSaltSize = 16

// GeneratePasswordHash hashes a password with argon2id and renders it in the
// PHC string format, so the parameters travel with the hash and can change
// later without invalidating stored rows.
func GeneratePasswordHash(password string) (string, error) {
	salt := make([]byte, argonSaltSize)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}

	c := defaultArgonConfig
	sum := argon2.IDKey([]byte(password), salt, c.Time, c.Memory, c.Threads, c.KeyLen)

	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, c.Memory, c.Time, c.Threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(sum),
	), nil
}

// ComparePasswordHash reports whether password matches the PHC-formatted hash.
// A malformed hash compares as a mismatch, never as an error the caller could
// mistake for "user not found".
func ComparePasswordHash(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	const partCount = 6
	if len(parts) != partCount || parts[1] != "argon2id" {
		return false
	}

	var c argonConfig
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &c.Memory, &c.Time, &c.Threads); err != nil {
		return false
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}

	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}

	// #nosec G115 - decoded argon2 hashes are short
	c.KeyLen = uint32(len(want))
	got := argon2.IDKey([]byte(password), salt, c.Time, c.Memory, c.Threads, c.KeyLen)

	return subtle.ConstantTimeCompare(want, got) == 1
}
