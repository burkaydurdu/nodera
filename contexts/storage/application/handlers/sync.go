package handlers

import (
	"context"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/storage/aggregates/storage"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/logger"
)

// syncer brings the engine to what the database says about a Storage: its
// size limit and whether it is stopped. The handlers call it after their
// commit and the reconcile job calls it for every Storage left behind, so an
// engine that was down catches up by itself.
type syncer struct {
	log          logger.ILog
	storages     storage.IService
	accounts     *accounts
	engine       Engine
	transactions postgresql.ITransactionManager
}

// apply asks the engine for what differs and records what it accepted. The
// row is not held during the engine call; what was applied is recorded as
// such, so a change made meanwhile shows as still out of sync.
func (y *syncer) apply(ctx context.Context, storageID uuid.UUID) error {
	s, err := y.storages.FindByID(ctx, storageID)
	if err != nil {
		return err
	}
	if s.Status != storage.StatusActive || s.InSync() {
		return nil
	}
	account, err := y.accounts.find(ctx, s.ProjectID)
	if err != nil {
		return err
	}

	appliedSize, appliedStopped := s.EngineSizeBytes, s.EngineStopped
	var failed error
	if s.EngineSizeBytes != s.Package.SizeBytes {
		if failed = y.engine.SetSizeLimit(ctx, account, s.BucketName, s.Package.SizeBytes); failed == nil {
			appliedSize = s.Package.SizeBytes
		}
	}
	if failed == nil && s.EngineStopped != s.Stopped() {
		if s.Stopped() {
			failed = y.engine.Stop(ctx, account, s.BucketName)
		} else {
			failed = y.engine.Resume(ctx, account, s.BucketName)
		}
		if failed == nil {
			appliedStopped = s.Stopped()
		}
	}
	if appliedSize == s.EngineSizeBytes && appliedStopped == s.EngineStopped {
		return engineError(failed)
	}

	txCtx, err := y.transactions.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = y.transactions.Rollback(txCtx) }()

	locked, err := y.storages.FindByIDForUpdate(txCtx, storageID)
	if err != nil {
		return err
	}
	locked.EngineSizeBytes, locked.EngineStopped = appliedSize, appliedStopped
	if err := y.storages.Save(txCtx, locked); err != nil {
		return err
	}
	if err := y.transactions.Commit(txCtx); err != nil {
		return err
	}
	if failed != nil {
		return engineError(failed)
	}

	return nil
}

// follow is apply for a handler that already answered the customer: the
// database is right, and a failure here is the reconcile job's to repeat.
func (y *syncer) follow(ctx context.Context, storageID uuid.UUID) {
	if err := y.apply(ctx, storageID); err != nil {
		y.log.ErrorWithFields(err, logger.NewFields().Add("storage_id", storageID))
	}
}
