package handlers

import (
	"context"

	"github.com/piggystats/nodera/contexts/customer/aggregates/apikey"
)

type APIKeySyncSummary struct {
	Written int
	Deleted int
}

// APIKeySync rewrites the Redis entries of the API keys from the database, so
// losing Redis is recoverable (docs/adr/0010). Writing an entry twice is
// harmless, so running it twice is too.
type APIKeySync struct {
	keys    apikey.IService
	entries IAPIKeyEntries
}

func NewAPIKeySync(keys apikey.IService, entries IAPIKeyEntries) *APIKeySync {
	return &APIKeySync{keys: keys, entries: entries}
}

// Run reads the revoked keys only after every active one is written: a key
// revoked while the run rewrites it is then deleted again, never left behind.
func (h *APIKeySync) Run(ctx context.Context) (*APIKeySyncSummary, error) {
	summary := &APIKeySyncSummary{}

	active, err := h.keys.FindAllActive(ctx)
	if err != nil {
		return nil, err
	}
	for _, k := range active {
		if err := h.entries.Write(ctx, k); err != nil {
			return nil, err
		}
		summary.Written++
	}

	revoked, err := h.keys.FindAllRevoked(ctx)
	if err != nil {
		return nil, err
	}
	for _, k := range revoked {
		if err := h.entries.Delete(ctx, k.ID); err != nil {
			return nil, err
		}
		summary.Deleted++
	}

	return summary, nil
}
