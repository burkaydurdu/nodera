// Package publickey parses the OpenSSH lines customers paste. It is kept
// apart from the aggregate's model so the request and dto types never pull
// golang.org/x/crypto into the packages the API documentation is built from.
package publickey

import (
	"crypto/rsa"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/piggystats/nodera/contexts/customer/aggregates/sshkey"
	"github.com/piggystats/nodera/utils/str"
)

// minRSABits is OpenSSH's own floor for ssh-rsa; shorter keys are refused
// by the Droplets the key is meant for.
const minRSABits = 2048

// Parsed is the canonical form of an accepted authorized_keys line.
type Parsed struct {
	// PublicKey is "<type> <base64>[ <comment>]", whatever spacing came in.
	PublicKey string
	// Fingerprint is "SHA256:<unpadded base64>", as ssh-keygen -l prints it.
	Fingerprint string
}

// Parse accepts one OpenSSH authorized_keys line of type ssh-ed25519 or
// ssh-rsa (>= 2048 bits) and answers its canonical text and fingerprint.
func Parse(line string) (*Parsed, error) {
	pub, comment, _, rest, err := ssh.ParseAuthorizedKey([]byte(strings.TrimSpace(line)))
	if err != nil || len(strings.TrimSpace(string(rest))) > 0 {
		return nil, sshkey.ErrorInvalidPublicKey
	}

	switch pub.Type() {
	case ssh.KeyAlgoED25519:
	case ssh.KeyAlgoRSA:
		if bits := rsaBits(pub); bits < minRSABits {
			return nil, sshkey.ErrorKeyTooShort.Copy().Add("bits", bits)
		}
	default:
		return nil, sshkey.ErrorUnsupportedKeyType.Copy().Add("type", pub.Type())
	}

	canonical := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub)))
	if comment = strings.TrimSpace(comment); str.IsNotEmpty(comment) {
		canonical += " " + comment
	}

	return &Parsed{PublicKey: canonical, Fingerprint: ssh.FingerprintSHA256(pub)}, nil
}

func rsaBits(pub ssh.PublicKey) int {
	cryptoKey, ok := pub.(ssh.CryptoPublicKey)
	if !ok {
		return 0
	}
	rsaKey, ok := cryptoKey.CryptoPublicKey().(*rsa.PublicKey)
	if !ok {
		return 0
	}

	return rsaKey.N.BitLen()
}
