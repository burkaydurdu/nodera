package sendingdomain

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"math/big"
)

const (
	keyBits       = 2048
	tokenLength   = 8
	tokenAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
)

// KeyPair is a fresh DKIM key before it is sealed.
type KeyPair struct {
	PrivatePKCS8 []byte
	PublicKey    string
}

func GenerateKey() (*KeyPair, error) {
	private, err := rsa.GenerateKey(rand.Reader, keyBits)
	if err != nil {
		return nil, err
	}

	pkcs8, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		return nil, err
	}
	public, err := x509.MarshalPKIXPublicKey(&private.PublicKey)
	if err != nil {
		return nil, err
	}

	return &KeyPair{PrivatePKCS8: pkcs8, PublicKey: base64.StdEncoding.EncodeToString(public)}, nil
}

// DKIMRecord is the TXT value the zone publishes for a key.
func DKIMRecord(publicKey string) string {
	return "v=DKIM1; k=rsa; p=" + publicKey
}

// NewToken is a domain's record token: short enough to read in a DNS panel,
// random enough that no two domains share one.
func NewToken() (string, error) {
	out := make([]byte, tokenLength)
	limit := big.NewInt(int64(len(tokenAlphabet)))
	for i := range out {
		n, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", err
		}
		out[i] = tokenAlphabet[n.Int64()]
	}

	return string(out), nil
}
