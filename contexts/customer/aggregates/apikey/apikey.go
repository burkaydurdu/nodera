// Package apikey is a secret a Team's software presents to call Nodera instead
// of a User signing in (docs/adr/0010). A key belongs to one Project, carries
// Permissions never wider than its creator's, and is shown whole only once:
// the row keeps the hash of its secret, never the secret.
package apikey

import (
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/pkg/authz"
)

const MaxActivePerProject = 20

type APIKey struct {
	// ID is public: it is part of the key itself and of every list.
	ID          string
	ProjectID   uuid.UUID
	TeamID      uuid.UUID
	Name        string
	SecretHash  string
	Permissions []authz.Permission
	CreatedBy   uuid.UUID
	CreatedAt   time.Time
	RevokedAt   *time.Time
}

type APIKeys []*APIKey

// Prefix names a key in a list without its secret.
func (k *APIKey) Prefix() string {
	return Marker + k.ID
}
