package handlers

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/storage/aggregates/access"
	"github.com/piggystats/nodera/contexts/storage/aggregates/storage"
	"github.com/piggystats/nodera/contexts/storage/aggregates/usage"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/logger"
)

// Measurer reads from the engine what each Storage holds and moves, keeps it
// and tells the Team when a Storage is nearly full or full.
type Measurer struct {
	log          logger.ILog
	access       access.IService
	storages     storage.IService
	usage        usage.IService
	accounts     *accounts
	engine       Engine
	transactions postgresql.ITransactionManager
	announcer    Announcer
	now          Clock
}

func NewMeasurer(log logger.ILog, accessService access.IService, storageService storage.IService, usageService usage.IService, accounts *accounts, eng Engine, transactions postgresql.ITransactionManager, announcer Announcer, now Clock) *Measurer {
	return &Measurer{log: log, access: accessService, storages: storageService, usage: usageService, accounts: accounts, engine: eng, transactions: transactions, announcer: announcer, now: now}
}

// Each visits every open Storage, a page at a time; one Storage's failure
// never holds up the others.
func (h *Measurer) Each(ctx context.Context, batch int, visit func(ctx context.Context, s *storage.Storage) error) error {
	after := uuid.Nil
	for {
		page, err := h.storages.ListActive(ctx, after, batch)
		if err != nil {
			return err
		}
		for _, s := range page {
			if err := visit(ctx, s); err != nil {
				h.log.ErrorWithFields(err, logger.NewFields().Add("storage_id", s.ID))
			}
			after = s.ID
		}
		if len(page) < batch {
			return nil
		}
	}
}

// Measure keeps what the engine counts and announces the marks it crossed.
func (h *Measurer) Measure(ctx context.Context, s *storage.Storage) error {
	account, err := h.accounts.find(ctx, s.ProjectID)
	if err != nil {
		return err
	}
	stats, err := h.engine.Stats(ctx, account, s.BucketName)
	if err != nil {
		return engineError(err)
	}
	now := h.now()

	var out announcements
	txCtx, err := h.transactions.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = h.transactions.Rollback(txCtx) }()

	locked, err := h.storages.FindByIDForUpdate(txCtx, s.ID)
	if err != nil {
		return err
	}
	if locked.Status != storage.StatusActive {
		return nil
	}
	nearly, full := locked.Measure(stats.Bytes, stats.Objects, now)
	if err := h.storages.Save(txCtx, locked); err != nil {
		return err
	}
	if err := h.usage.UpsertSize(txCtx, &usage.Size{StorageID: s.ID, Day: usage.DayOf(now), UsedBytes: stats.Bytes, ObjectCount: stats.Objects}); err != nil {
		return err
	}
	if nearly || full {
		project, err := h.access.FindProject(txCtx, s.ProjectID)
		if err != nil {
			return err
		}
		measured := *locked
		if nearly {
			out.add(func(ctx context.Context) error { return h.announcer.NearlyFull(ctx, project, &measured, now) })
		}
		if full {
			out.add(func(ctx context.Context) error { return h.announcer.Full(ctx, project, &measured, now) })
		}
		out.add(func(ctx context.Context) error {
			return h.announcer.StorageChanged(ctx, s.TeamID, s.ProjectID, s.ID, now)
		})
	}
	if err := h.transactions.Commit(txCtx); err != nil {
		return err
	}
	out.publish(ctx, h.log)

	return nil
}

// Traffic reads the usage log from where it last stopped. The hour before
// the current one is read again next time, because the log is still being
// written when an hour has just ended.
func (h *Measurer) Traffic(ctx context.Context, s *storage.Storage) error {
	account, err := h.accounts.find(ctx, s.ProjectID)
	if err != nil {
		return err
	}
	now := h.now()
	from := usage.HourOf(s.CreatedAt)
	if s.TrafficReadUntil != nil {
		from = *s.TrafficReadUntil
	}
	hours, err := h.engine.Traffic(ctx, account, s.BucketName, from, now)
	if err != nil {
		return engineError(err)
	}
	rows := make([]*usage.Traffic, 0, len(hours))
	for _, t := range hours {
		rows = append(rows, &usage.Traffic{StorageID: s.ID, Hour: usage.HourOf(t.Hour), BytesSent: t.BytesSent, BytesReceived: t.BytesReceived, Requests: t.Requests})
	}

	txCtx, err := h.transactions.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = h.transactions.Rollback(txCtx) }()

	if err := h.usage.UpsertTraffic(txCtx, rows); err != nil {
		return err
	}
	locked, err := h.storages.FindByIDForUpdate(txCtx, s.ID)
	if err != nil {
		return err
	}
	readUntil := usage.HourOf(now).Add(-time.Hour)
	if readUntil.Before(from) {
		readUntil = from
	}
	locked.TrafficReadUntil = &readUntil
	if err := h.storages.Save(txCtx, locked); err != nil {
		return err
	}

	return h.transactions.Commit(txCtx)
}
