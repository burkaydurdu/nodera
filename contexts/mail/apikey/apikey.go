// Package apikey checks the API keys the customer context issues
// (docs/adr/0010). The customer context writes each key to Redis and deletes
// it on revocation; this context reads the same entry, the way the services
// check a Session, so a revoked key fails on the next request.
package apikey

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/pkg/authz"
	"github.com/piggystats/nodera/utils/hash"
	"github.com/piggystats/nodera/utils/str"
)

// The Redis keys are a wire contract with the customer context and the
// integration gateway, declared here rather than imported.
const (
	KeyFormat      = "api_key:%s"
	LastUsedFormat = "api_key_last_used:%s"
	prefix         = "nd_"
	idLength       = 12
)

// Key is an authenticated API key: the Project it acts on and what it may do.
type Key struct {
	ID          string
	ProjectID   uuid.UUID
	TeamID      uuid.UUID
	Permissions []authz.Permission
}

func (k *Key) Allows(permission authz.Permission) bool {
	return slices.Contains(k.Permissions, permission)
}

type entry struct {
	Hash        string    `json:"hash"`
	ProjectID   uuid.UUID `json:"project_id"`
	TeamID      uuid.UUID `json:"team_id"`
	Permissions []string  `json:"permissions"`
}

type Cache interface {
	// GetString answers "" for an absent key.
	GetString(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value any, expiration time.Duration) error
}

type Verifier struct {
	cache Cache
	now   func() time.Time
}

func NewVerifier(cache Cache) *Verifier {
	return &Verifier{cache: cache, now: time.Now}
}

// WithClock swaps the clock the last-used mark is written with.
func (v *Verifier) WithClock(now func() time.Time) *Verifier {
	v.now = now

	return v
}

// Verify answers the key when raw is a live key holding the Permission. Every
// failure to authenticate is the same 401, so a caller learns nothing about
// which part was wrong. A store that cannot be read is not the caller's
// fault: it answers ErrorUnavailable, so a sending server retries a good key
// instead of giving up on it during an outage.
func (v *Verifier) Verify(ctx context.Context, raw string, permission authz.Permission) (*Key, error) {
	id, secret, ok := Parse(raw)
	if !ok {
		return nil, ErrorUnauthorized
	}

	stored, err := v.cache.GetString(ctx, fmt.Sprintf(KeyFormat, id))
	if err != nil {
		return nil, ErrorUnavailable.Copy().SetError(err)
	}
	if str.IsEmpty(stored) {
		return nil, ErrorUnauthorized
	}

	var e entry
	if err := json.Unmarshal([]byte(stored), &e); err != nil {
		return nil, ErrorUnavailable.Copy().SetError(err)
	}

	if !hash.ConstantTimeEquals(hash.SHA256Hex(secret), strings.ToLower(e.Hash)) {
		return nil, ErrorUnauthorized
	}

	key := &Key{ID: id, ProjectID: e.ProjectID, TeamID: e.TeamID}
	for _, p := range e.Permissions {
		key.Permissions = append(key.Permissions, authz.Permission(p))
	}
	if !key.Allows(permission) {
		return nil, ErrorForbidden
	}

	// The mark is a convenience for the Team's key list; failing to write it
	// never refuses a request.
	_ = v.cache.Set(ctx, fmt.Sprintf(LastUsedFormat, id), v.now().UTC().Format(time.RFC3339Nano), 0)

	return key, nil
}

// Parse splits "nd_<id>.<secret>"; the id is the public half.
func Parse(raw string) (id, secret string, ok bool) {
	if !strings.HasPrefix(raw, prefix) {
		return "", "", false
	}

	id, secret, found := strings.Cut(strings.TrimPrefix(raw, prefix), ".")
	if !found || len(id) != idLength || !base62(id) || str.IsEmpty(secret) || !base62(secret) {
		return "", "", false
	}

	return id, secret, true
}

func base62(s string) bool {
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return false
		}
	}

	return true
}
