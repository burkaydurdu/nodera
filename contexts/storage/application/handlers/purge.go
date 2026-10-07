package handlers

import (
	"context"

	"github.com/piggystats/nodera/contexts/storage/aggregates/accesskey"
	"github.com/piggystats/nodera/contexts/storage/aggregates/storage"
	"github.com/piggystats/nodera/contexts/storage/aggregates/usage"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/logger"
)

// Purger finishes deletions: the engine removes the bucket with everything in
// it, and only then is the Storage gone and its Bucket name free.
type Purger struct {
	log          logger.ILog
	storages     storage.IService
	keys         accesskey.IService
	usage        usage.IService
	accounts     *accounts
	engine       Engine
	transactions postgresql.ITransactionManager
	announcer    Announcer
	now          Clock
}

func NewPurger(log logger.ILog, storageService storage.IService, keyService accesskey.IService, usageService usage.IService, accounts *accounts, eng Engine, transactions postgresql.ITransactionManager, announcer Announcer, now Clock) *Purger {
	return &Purger{log: log, storages: storageService, keys: keyService, usage: usageService, accounts: accounts, engine: eng, transactions: transactions, announcer: announcer, now: now}
}

func (h *Purger) Due(ctx context.Context, limit int) ([]*storage.Storage, error) {
	return h.storages.ListDeleting(ctx, limit)
}

func (h *Purger) Purge(ctx context.Context, s *storage.Storage) error {
	account, err := h.accounts.find(ctx, s.ProjectID)
	if err != nil {
		return err
	}
	if err := h.engine.DeleteBucket(ctx, account, s.BucketName); err != nil {
		return engineError(err)
	}
	now := h.now()

	txCtx, err := h.transactions.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = h.transactions.Rollback(txCtx) }()

	if err := h.keys.DeleteByStorage(txCtx, s.ID); err != nil {
		return err
	}
	if err := h.usage.DeleteByStorage(txCtx, s.ID); err != nil {
		return err
	}
	if err := h.storages.MarkDeleted(txCtx, s.ID, now); err != nil {
		return err
	}
	if err := h.transactions.Commit(txCtx); err != nil {
		return err
	}
	if err := h.announcer.StorageChanged(ctx, s.TeamID, s.ProjectID, s.ID, now); err != nil {
		h.log.Error(err)
	}

	return nil
}

// Abandon removes a reservation whose request never came back: whatever the
// engine may have made for it goes too, and the Bucket name is free again.
func (h *Purger) Abandon(ctx context.Context, s *storage.Storage) error {
	account, err := h.accounts.find(ctx, s.ProjectID)
	switch {
	case err == nil:
		if err := h.engine.DeleteBucket(ctx, account, s.BucketName); err != nil {
			return engineError(err)
		}
	case !isNoAccount(err):
		return err
	}

	return h.storages.Release(ctx, s.ID)
}
