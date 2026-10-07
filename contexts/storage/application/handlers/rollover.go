package handlers

import (
	"context"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/storage/aggregates/access"
	"github.com/piggystats/nodera/contexts/storage/aggregates/storage"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/logger"
)

// Rollover applies the smaller Packages due at a Period's first instant. A
// change applies at that instant, not when the job runs, so billing's
// intervals meet exactly at the boundary.
type Rollover struct {
	log          logger.ILog
	access       access.IService
	storages     storage.IService
	transactions postgresql.ITransactionManager
	announcer    Announcer
	sync         *syncer
	now          Clock
}

func NewRollover(log logger.ILog, accessService access.IService, storageService storage.IService, transactions postgresql.ITransactionManager, announcer Announcer, sync *syncer, now Clock) *Rollover {
	return &Rollover{log: log, access: accessService, storages: storageService, transactions: transactions, announcer: announcer, sync: sync, now: now}
}

func (h *Rollover) Due(ctx context.Context, limit int) ([]uuid.UUID, error) {
	return h.storages.ListScheduledDue(ctx, storage.PeriodStart(h.now()), limit)
}

// Carry applies the Storage's scheduled change at the instant it was due if
// it still fits; otherwise the change is dropped, the Storage stays on its
// Package and the Team is told.
func (h *Rollover) Carry(ctx context.Context, storageID uuid.UUID) error {
	var out announcements
	txCtx, err := h.transactions.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = h.transactions.Rollback(txCtx) }()

	s, err := h.storages.FindByIDForUpdate(txCtx, storageID)
	if err != nil {
		return err
	}
	if s.Status != storage.StatusActive || s.Scheduled == nil || s.Scheduled.Due().After(h.now()) {
		return nil
	}
	start := s.Scheduled.Due()
	project, err := h.access.FindProject(txCtx, s.ProjectID)
	if err != nil {
		return err
	}

	scheduled := *s.Scheduled
	if s.Fits(scheduled.SizeBytes) {
		old := s.Package
		s.Hold(scheduled.HoldFrom(start))
		held := *s
		out.add(func(ctx context.Context) error {
			return h.announcer.PackageStopped(ctx, s.TeamID, s.ID, old.StartedAt, start)
		})
		out.add(func(ctx context.Context) error { return h.announcer.PackageStarted(ctx, project, &held) })
	} else {
		s.Scheduled = nil
		kept := *s
		out.add(func(ctx context.Context) error {
			return h.announcer.DowngradeSkipped(ctx, project, &kept, &scheduled, start)
		})
	}
	if err := h.storages.Save(txCtx, s); err != nil {
		return err
	}
	out.add(func(ctx context.Context) error {
		return h.announcer.StorageChanged(ctx, s.TeamID, s.ProjectID, s.ID, start)
	})

	if err := h.transactions.Commit(txCtx); err != nil {
		return err
	}
	out.publish(ctx, h.log)
	h.sync.follow(ctx, storageID)

	return nil
}
