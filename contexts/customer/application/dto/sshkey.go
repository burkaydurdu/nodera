package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/customer/aggregates/sshkey"
)

type SSHKey struct {
	ID          uuid.UUID `json:"id"`
	TeamID      uuid.UUID `json:"team_id"`
	Name        string    `json:"name"`
	PublicKey   string    `json:"public_key"`
	Fingerprint string    `json:"fingerprint"`
	CreatedAt   time.Time `json:"created_at"`
}

func ToSSHKey(k *sshkey.SSHKey) *SSHKey {
	return &SSHKey{ID: k.ID, TeamID: k.TeamID, Name: k.Name, PublicKey: k.PublicKey, Fingerprint: k.Fingerprint, CreatedAt: k.CreatedAt}
}

func ToSSHKeys(keys sshkey.SSHKeys) []*SSHKey {
	out := make([]*SSHKey, 0, len(keys))
	for _, k := range keys {
		out = append(out, ToSSHKey(k))
	}

	return out
}
