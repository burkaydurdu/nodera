package signing_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/piggystats/nodera/contexts/billing/signing"
)

func TestVerify(t *testing.T) {
	signer := signing.New("secret")
	id := uuid.New()
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	expires := now.Add(time.Hour)
	token := signer.Sign(signing.PurposeInvoicePDF, id, expires)

	assert.True(t, signer.Verify(signing.PurposeInvoicePDF, id, expires, token, now))
	assert.False(t, signer.Verify(signing.PurposeInvoicePDF, id, expires, token, expires), "expired at the instant")
	assert.False(t, signer.Verify(signing.PurposeInvoicePDF, id, expires.Add(time.Hour), token, now), "extended expiry")
	assert.False(t, signer.Verify(signing.PurposeInvoicePDF, uuid.New(), expires, token, now), "another object")
	assert.False(t, signer.Verify(signing.PurposePaymentConfirm, id, expires, token, now), "another purpose")
	assert.False(t, signing.New("other").Verify(signing.PurposeInvoicePDF, id, expires, token, now), "another key")
}
