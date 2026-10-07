// Package secrets seals what a context must read back but never store in
// clear: mail's DKIM private keys and Webhook signing secrets, storage's
// account root keys. It also draws the keys of other purposes from the same
// configured key. It lives outside the contexts because a context never
// imports another.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
)

const keyBytes = 32

var ErrSealedTooShort = errors.New("sealed value is shorter than its nonce")

// Sealer is AES-256-GCM under one key from configuration; each value carries
// its own random nonce in front of the ciphertext.
type Sealer struct {
	aead cipher.AEAD
}

// NewSealerFromBase64 reads the 32-byte key the way the TOML carries it.
func NewSealerFromBase64(encoded string) (*Sealer, error) {
	key, err := decodeKey(encoded)
	if err != nil {
		return nil, err
	}

	return NewSealer(key)
}

// DeriveFromBase64 is a key of its own for purpose, drawn from the configured
// key, so the TOML carries one secret and no derived key reveals it or
// another.
func DeriveFromBase64(encoded, purpose string) ([]byte, error) {
	key, err := decodeKey(encoded)
	if err != nil {
		return nil, err
	}
	if len(key) != keyBytes {
		return nil, fmt.Errorf("encryption key must be %d bytes, got %d", keyBytes, len(key))
	}

	return hkdf.Key(sha256.New, key, nil, purpose, keyBytes)
}

func decodeKey(encoded string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("encryption key is not base64: %w", err)
	}

	return key, nil
}

func NewSealer(key []byte) (*Sealer, error) {
	if len(key) != keyBytes {
		return nil, fmt.Errorf("encryption key must be %d bytes, got %d", keyBytes, len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	return &Sealer{aead: aead}, nil
}

func (s *Sealer) Seal(plain []byte) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}

	return s.aead.Seal(nonce, nonce, plain, nil), nil
}

func (s *Sealer) Open(sealed []byte) ([]byte, error) {
	size := s.aead.NonceSize()
	if len(sealed) < size {
		return nil, ErrSealedTooShort
	}

	return s.aead.Open(nil, sealed[:size], sealed[size:], nil)
}
