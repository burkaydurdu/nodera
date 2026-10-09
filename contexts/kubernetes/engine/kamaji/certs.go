package kamaji

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/piggystats/nodera/contexts/kubernetes/engine"
)

func parseCertificate(raw []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, errors.New("kamaji: a certificate is not pem")
	}

	return x509.ParseCertificate(block.Bytes)
}

func parsePrivateKey(raw []byte) (crypto.Signer, error) {
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, errors.New("kamaji: a private key is not pem")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		signer, ok := key.(crypto.Signer)
		if !ok {
			return nil, errors.New("kamaji: the ca key cannot sign")
		}

		return signer, nil
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	if key, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return key, nil
	}

	return nil, errors.New("kamaji: the ca key is in no known format")
}

// caHash is kubeadm's pin of a CA: the SHA-256 of its public key info.
func caHash(ca *x509.Certificate) string {
	sum := sha256.Sum256(ca.RawSubjectPublicKeyInfo)

	return "sha256:" + hex.EncodeToString(sum[:])
}

// signClient cuts a client certificate for the Kubeconfig's identity: the
// user as the common name, the groups as organizations, good for the TTL.
func signClient(ca *x509.Certificate, caKey any, spec engine.KubeconfigSpec) (certPEM, keyPEM []byte, err error) {
	signer, ok := caKey.(crypto.Signer)
	if !ok {
		return nil, nil, errors.New("kamaji: the ca key cannot sign")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return nil, nil, err
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: spec.User, Organization: spec.Groups},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(spec.TTL),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, signer)
	if err != nil {
		return nil, nil, fmt.Errorf("kamaji: signing the kubeconfig certificate: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, err
	}

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), nil
}

const tokenAlphabet = "0123456789abcdefghijklmnopqrstuvwxyz"

// randomToken is a kubeadm token part: lower-case letters and digits.
func randomToken(n int) string {
	out := make([]byte, n)
	for i := range out {
		idx, _ := rand.Int(rand.Reader, big.NewInt(int64(len(tokenAlphabet))))
		out[i] = tokenAlphabet[idx.Int64()]
	}

	return string(out)
}
