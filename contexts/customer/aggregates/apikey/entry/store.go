// Package entry is how an API key reaches its readers: the integration
// gateway and the mail service authenticate a key from Redis, never from the
// customer database (docs/adr/0010). The record is a wire contract those
// binaries read, so its shape changes only together with them.
package entry

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/customer/aggregates/apikey"
	"github.com/piggystats/nodera/pkg/authz"
	"github.com/piggystats/nodera/utils/str"
)

const (
	// KeyFormat holds a key's record from its creation to its revocation; a
	// reader takes a missing record for a revoked or unknown key.
	KeyFormat = "api_key:%s"
	// LastUsedKeyFormat is written by whoever authenticates the key; this
	// context only reads it.
	LastUsedKeyFormat = "api_key_last_used:%s"
)

type Cache interface {
	Set(ctx context.Context, key string, value any, expiration time.Duration) error
	GetString(ctx context.Context, key string) (string, error)
	Delete(ctx context.Context, keys ...string) error
}

// Entry is the record. Hash covers the secret part alone: a reader hashes what
// follows the dot of nd_<id>.<secret>.
type Entry struct {
	Hash        string             `json:"hash"`
	ProjectID   uuid.UUID          `json:"project_id"`
	TeamID      uuid.UUID          `json:"team_id"`
	Permissions []authz.Permission `json:"permissions"`
}

type Store struct {
	cache Cache
}

func NewStore(cache Cache) *Store {
	return &Store{cache: cache}
}

// Write publishes a key's record without expiry: a key lives until it is
// revoked.
func (s *Store) Write(ctx context.Context, key *apikey.APIKey) error {
	payload, err := json.Marshal(Entry{Hash: key.SecretHash, ProjectID: key.ProjectID, TeamID: key.TeamID, Permissions: key.Permissions})
	if err != nil {
		return err
	}

	return s.cache.Set(ctx, fmt.Sprintf(KeyFormat, key.ID), string(payload), 0)
}

func (s *Store) Delete(ctx context.Context, id string) error {
	return s.cache.Delete(ctx, fmt.Sprintf(KeyFormat, id))
}

// LastUsed answers when each key last authenticated. A key never used is
// absent, and so is a value that does not parse: a list must not fail over
// another binary's bad write.
func (s *Store) LastUsed(ctx context.Context, ids []string) (map[string]time.Time, error) {
	used := make(map[string]time.Time, len(ids))
	for _, id := range ids {
		raw, err := s.cache.GetString(ctx, fmt.Sprintf(LastUsedKeyFormat, id))
		if err != nil {
			return nil, err
		}
		if str.IsEmpty(raw) {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			continue
		}
		used[id] = at.UTC()
	}

	return used, nil
}
