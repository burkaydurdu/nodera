package handlers

import (
	"context"
	"time"

	"github.com/piggystats/nodera/contexts/mail/aggregates/access"
	"github.com/piggystats/nodera/contexts/mail/aggregates/limits"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/logger"
)

// Protection is what keeps one Project from harming Nodera's shared IPs: the
// Daily limit grows only with clean sending, and rates over the line warn the
// Team and then Pause the Project (docs/adr/0009).
type Protection struct {
	log          logger.ILog
	access       access.IService
	limits       limits.IService
	transactions postgresql.ITransactionManager
	announcer    Announcer
	now          Clock
}

func NewProtection(
	log logger.ILog,
	accessService access.IService,
	limitService limits.IService,
	transactions postgresql.ITransactionManager,
	announcer Announcer,
	now Clock,
) *Protection {
	return &Protection{log: log, access: accessService, limits: limitService, transactions: transactions, announcer: announcer, now: now}
}

// EvaluateDays decides the Daily limit after each finished day, in order, so
// days a stopped worker missed are caught up one by one, each doubling the
// limit the one before it left.
func (h *Protection) EvaluateDays(ctx context.Context, batch int) error {
	now := h.now()
	days, err := h.limits.ListUnevaluated(ctx, limits.DayOf(now), batch)
	if err != nil {
		return err
	}

	for _, day := range days {
		if err := h.evaluate(ctx, day, now); err != nil {
			return err
		}
	}

	return nil
}

// evaluate marks the day decided in the same transaction that doubles the
// limit, so a crash between the two never doubles it twice.
func (h *Protection) evaluate(ctx context.Context, day *limits.Day, now time.Time) error {
	txCtx, err := h.transactions.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = h.transactions.Rollback(txCtx) }()

	owed, err := h.limits.MarkEvaluated(txCtx, day.ProjectID, day.Day, now)
	if err != nil || !owed {
		return err
	}
	if day.Clean() {
		if err := h.limits.DoubleDailyLimit(txCtx, day.ProjectID); err != nil {
			return err
		}
	}

	return h.transactions.Commit(txCtx)
}

// JudgeRates looks at every Project that sent in the window.
func (h *Protection) JudgeRates(ctx context.Context) error {
	now := h.now()
	today := limits.DayOf(now)
	from := today.AddDate(0, 0, 1-limits.WindowDays)

	active, err := h.limits.ListActiveSince(ctx, from)
	if err != nil {
		return err
	}
	for _, lim := range active {
		if lim.Paused() {
			continue
		}
		judgedFrom := lim.JudgedFrom(from)
		if judgedFrom.After(today) {
			continue
		}
		if err := h.judge(ctx, lim, judgedFrom, today); err != nil {
			h.log.ErrorWithFields(err, logger.NewFields().Add("project_id", lim.ProjectID))
		}
	}

	return nil
}

func (h *Protection) judge(ctx context.Context, lim *limits.Limits, from, to time.Time) error {
	window, err := h.limits.Window(ctx, lim.ProjectID, from, to)
	if err != nil {
		return err
	}

	verdict := limits.Judge(window)
	if !verdict.Pause && !verdict.Warn {
		return nil
	}

	now := h.now()
	project, err := h.access.FindProject(ctx, lim.ProjectID)
	if err != nil {
		return err
	}

	var out announcements
	if verdict.Pause {
		paused, err := h.limits.Pause(ctx, lim.ProjectID, verdict.Reason, now)
		if err != nil || !paused {
			return err
		}
		out.add(func(ctx context.Context) error {
			return h.announcer.SendingPaused(ctx, project, verdict.Reason, window, now)
		})
		out.add(func(ctx context.Context) error {
			return h.announcer.SendingChanged(ctx, project.TeamID, project.ProjectID, now)
		})
	} else {
		warned, err := h.limits.MarkRatesWarned(ctx, lim.ProjectID, now)
		if err != nil || !warned {
			return err
		}
		out.add(func(ctx context.Context) error { return h.announcer.RatesWarning(ctx, project, window, now) })
	}
	out.publish(ctx, h.log)

	return nil
}
