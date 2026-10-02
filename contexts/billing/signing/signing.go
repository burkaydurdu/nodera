// Package signing makes the links billing emails carry: an HMAC over what
// the link is for, the object it names and its expiry, so the link works
// without a session and cannot be bent to another object or extended.
package signing

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// Purpose separates the link kinds, so a token for one never opens another.
type Purpose string

const (
	PurposeInvoicePDF     Purpose = "invoice-pdf"
	PurposePaymentConfirm Purpose = "payment-confirm"
)

type Signer struct {
	key []byte
}

func New(secret string) *Signer {
	return &Signer{key: []byte(secret)}
}

// Sign answers the hex token for the object until expires.
func (s *Signer) Sign(purpose Purpose, id uuid.UUID, expires time.Time) string {
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(string(purpose) + "|" + id.String() + "|" + strconv.FormatInt(expires.Unix(), 10)))

	return hex.EncodeToString(mac.Sum(nil))
}

// Verify accepts a token made by Sign for the same purpose, object and
// expiry, while the expiry is still ahead of now.
func (s *Signer) Verify(purpose Purpose, id uuid.UUID, expires time.Time, token string, now time.Time) bool {
	if !now.Before(expires) {
		return false
	}

	return hmac.Equal([]byte(token), []byte(s.Sign(purpose, id, expires)))
}
