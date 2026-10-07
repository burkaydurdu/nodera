package handlers

import (
	"context"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/storage/aggregates/access"
	"github.com/piggystats/nodera/contexts/storage/aggregates/storage"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/logger"
)

// Operations is what Nodera's operators do until the Admin side exists: Pause
// a Storage after a complaint and lift the Pause.
type Operations struct {
	log          logger.ILog
	access       access.IService
	storages     storage.IService
	transactions postgresql.ITransactionManager
	announcer    Announcer
	sync         *syncer
	now          Clock
}

func NewOperations(log logger.ILog, accessService access.IService, storageService storage.IService, transactions postgresql.ITransactionManager, announcer Announcer, sync *syncer, now Clock) *Operations {
	return &Operations{log: log, access: accessService, storages: storageService, transactions: transactions, announcer: announcer, sync: sync, now: now}
}

// Pause stops the Storage for the reason; it answers false when it was
// already Paused.
func (h *Operations) Pause(ctx context.Context, storageID uuid.UUID, reason storage.Reason) (bool, error) {
	if !reason.IsValid() {
		return false, storage.ErrorInvalidReason
	}
	var out announcements
	paused := false
	err := h.change(ctx, storageID, func(ctx context.Context, s *storage.Storage, project *access.ProjectAccess) {
		if s.Pause != nil {
			return
		}
		pause := storage.Pause{Reason: reason, At: h.now()}
		s.Pause = &pause
		paused = true
		out.add(func(ctx context.Context) error { return h.announcer.Paused(ctx, project, s, pause) })
		out.add(func(ctx context.Context) error {
			return h.announcer.StorageChanged(ctx, s.TeamID, s.ProjectID, s.ID, pause.At)
		})
	}, &out)

	return paused, err
}

// Unpause lifts a Pause; it answers false when there was none. A suspended
// Team's Storage stays stopped until the Team pays.
func (h *Operations) Unpause(ctx context.Context, storageID uuid.UUID) (bool, error) {
	var out announcements
	lifted := false
	err := h.change(ctx, storageID, func(ctx context.Context, s *storage.Storage, _ *access.ProjectAccess) {
		if s.Pause == nil {
			return
		}
		s.Pause = nil
		lifted = true
		now := h.now()
		out.add(func(ctx context.Context) error {
			return h.announcer.StorageChanged(ctx, s.TeamID, s.ProjectID, s.ID, now)
		})
	}, &out)

	return lifted, err
}

func (h *Operations) change(ctx context.Context, storageID uuid.UUID, fn func(ctx context.Context, s *storage.Storage, project *access.ProjectAccess), out *announcements) error {
	txCtx, err := h.transactions.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = h.transactions.Rollback(txCtx) }()

	s, err := h.storages.FindByIDForUpdate(txCtx, storageID)
	if err != nil {
		return err
	}
	project, err := h.access.FindProject(txCtx, s.ProjectID)
	if err != nil {
		return err
	}
	fn(txCtx, s, project)
	if err := h.storages.Save(txCtx, s); err != nil {
		return err
	}
	if err := h.transactions.Commit(txCtx); err != nil {
		return err
	}
	out.publish(ctx, h.log)
	h.sync.follow(ctx, storageID)

	return nil
}
