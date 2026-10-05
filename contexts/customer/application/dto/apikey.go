package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/customer/aggregates/apikey"
)

// APIKey is a key as a list shows it: never its secret. LastUsedAt is when a
// reader last authenticated it, null for a key never used.
type APIKey struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Prefix      string     `json:"prefix"`
	Permissions []string   `json:"permissions"`
	CreatedBy   uuid.UUID  `json:"created_by"`
	CreatedAt   time.Time  `json:"created_at"`
	LastUsedAt  *time.Time `json:"last_used_at"`
}

// CreatedAPIKey is the one answer that carries the whole key.
type CreatedAPIKey struct {
	APIKey
	Key string `json:"key"`
}

func ToAPIKey(k *apikey.APIKey, lastUsedAt *time.Time) *APIKey {
	permissions := make([]string, 0, len(k.Permissions))
	for _, p := range k.Permissions {
		permissions = append(permissions, string(p))
	}

	return &APIKey{
		ID:          k.ID,
		Name:        k.Name,
		Prefix:      k.Prefix(),
		Permissions: permissions,
		CreatedBy:   k.CreatedBy,
		CreatedAt:   k.CreatedAt.UTC(),
		LastUsedAt:  lastUsedAt,
	}
}

func ToAPIKeys(keys apikey.APIKeys, lastUsed map[string]time.Time) []*APIKey {
	out := make([]*APIKey, 0, len(keys))
	for _, k := range keys {
		var lastUsedAt *time.Time
		if at, ok := lastUsed[k.ID]; ok {
			lastUsedAt = &at
		}
		out = append(out, ToAPIKey(k, lastUsedAt))
	}

	return out
}

func ToCreatedAPIKey(k *apikey.APIKey, fullKey string) *CreatedAPIKey {
	return &CreatedAPIKey{APIKey: *ToAPIKey(k, nil), Key: fullKey}
}
