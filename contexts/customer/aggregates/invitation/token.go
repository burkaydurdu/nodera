package invitation

import (
	"crypto/rand"
	"encoding/base64"
	"strings"

	"github.com/piggystats/nodera/utils/hash"
)

const tokenBytes = 32

// NewToken draws the secret the accept link carries and the hash the row
// keeps. Only the hash is ever stored, so a leaked table hands out nothing.
func NewToken() (raw, tokenHash string, err error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", "", ErrorCannotCreate.Copy().SetError(err)
	}
	raw = base64.RawURLEncoding.EncodeToString(b)

	return raw, HashToken(raw), nil
}

func HashToken(raw string) string {
	return hash.SHA256Hex(raw)
}

// MaskEmail shows enough of the address for the invitee to recognise it on
// the public accept page without handing the whole address to whoever holds
// the link: "b***@example.com".
func MaskEmail(email string) string {
	at := strings.LastIndex(email, "@")
	if at <= 0 {
		return "***"
	}

	return email[:1] + "***" + email[at:]
}

func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
