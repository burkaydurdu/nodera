package handlers

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/storage/aggregates/storage"
)

// abandonedAfter is how long a reservation may stay open before no request
// is believed to be finishing it.
const abandonedAfter = 10 * time.Minute

// Reconciler is the job that makes the engine agree with the database after
// anything went wrong: Storages whose size limit or stop was not applied, and
// reservations left behind by a crash.
type Reconciler struct {
	storages storage.IService
	sync     *syncer
	purger   *Purger
	now      Clock
}

func NewReconciler(storageService storage.IService, sync *syncer, purger *Purger, now Clock) *Reconciler {
	return &Reconciler{storages: storageService, sync: sync, purger: purger, now: now}
}

func (h *Reconciler) OutOfSync(ctx context.Context, limit int) ([]uuid.UUID, error) {
	return h.storages.ListOutOfSync(ctx, limit)
}

func (h *Reconciler) Apply(ctx context.Context, storageID uuid.UUID) error {
	return h.sync.apply(ctx, storageID)
}

func (h *Reconciler) Abandoned(ctx context.Context, limit int) ([]*storage.Storage, error) {
	return h.storages.ListAbandoned(ctx, h.now().Add(-abandonedAfter), limit)
}

func (h *Reconciler) Abandon(ctx context.Context, s *storage.Storage) error {
	return h.purger.Abandon(ctx, s)
}

func isNoAccount(err error) bool {
	return errors.Is(err, errNoAccount)
}
