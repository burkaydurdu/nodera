package handlers

import (
	"context"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/mail/aggregates/access"
	"github.com/piggystats/nodera/contexts/mail/aggregates/packages"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/logger"
)

// Rollover carries every Project into the new Period: scheduled changes take
// effect and the allowance renews.
type Rollover struct {
	log          logger.ILog
	packages     packages.IService
	periods      *periods
	transactions postgresql.ITransactionManager
	now          Clock
}

func NewRollover(log logger.ILog, accessService access.IService, packageService packages.IService, transactions postgresql.ITransactionManager, announcer Announcer, now Clock) *Rollover {
	return &Rollover{
		log: log, packages: packageService, transactions: transactions, now: now,
		periods: &periods{access: accessService, packages: packageService, announcer: announcer},
	}
}

func (h *Rollover) Due(ctx context.Context, limit int) ([]uuid.UUID, error) {
	return h.packages.ListRolloverDue(ctx, packages.PeriodStart(h.now()), limit)
}

func (h *Rollover) Carry(ctx context.Context, projectID uuid.UUID) error {
	var out announcements
	txCtx, err := h.transactions.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = h.transactions.Rollback(txCtx) }()

	if _, _, err := h.periods.ensure(txCtx, projectID, h.now(), &out); err != nil {
		return err
	}
	if err := h.transactions.Commit(txCtx); err != nil {
		return err
	}
	out.publish(ctx, h.log)

	return nil
}
