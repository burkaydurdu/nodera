package droplet

import (
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"
)

// ParseSSHKeys verifies each key is a parseable OpenSSH public key and
// computes its SHA-256 fingerprint. The service never trusts a fingerprint
// sent by a client.
func ParseSSHKeys(inputs []SSHKeyInput) ([]SSHKey, error) {
	keys := make([]SSHKey, 0, len(inputs))
	seen := make(map[string]struct{}, len(inputs))
	for _, in := range inputs {
		parsed, _, _, _, err := ssh.ParseAuthorizedKey([]byte(strings.TrimSpace(in.PublicKey)))
		if err != nil {
			return nil, ErrorInvalidSSHKey.Copy().SetMessage(fmt.Sprintf("ssh key %q is not a valid OpenSSH public key", in.Name)).SetError(err)
		}

		fingerprint := ssh.FingerprintSHA256(parsed)
		if _, dup := seen[fingerprint]; dup {
			return nil, ErrorInvalidSSHKey.Copy().SetMessage(fmt.Sprintf("ssh key %q is listed twice", in.Name))
		}
		seen[fingerprint] = struct{}{}

		keys = append(keys, SSHKey{Name: in.Name, Fingerprint: fingerprint})
	}

	return keys, nil
}
