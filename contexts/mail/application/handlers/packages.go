package handlers

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/mail/aggregates/access"
	"github.com/piggystats/nodera/contexts/mail/aggregates/billing"
	"github.com/piggystats/nodera/contexts/mail/aggregates/limits"
	"github.com/piggystats/nodera/contexts/mail/aggregates/packages"
	"github.com/piggystats/nodera/contexts/mail/application/dto"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/authz"
)

// Sending states, in the order the send path refuses.
const (
	stateSuspended  = "suspended"
	stateNoPackage  = "no_package"
	statePaused     = "paused"
	stateExhausted  = "exhausted"
	stateDailyLimit = "daily_limit"
	stateActive     = "active"
)

// Packages is a Project's Mail overview and the choice of its Package.
type Packages struct {
	authorizer
	log          logger.ILog
	billing      billing.IService
	packages     packages.IService
	limits       limits.IService
	periods      *periods
	transactions postgresql.ITransactionManager
	announcer    Announcer
	now          Clock
}

func NewPackages(
	log logger.ILog,
	accessService access.IService,
	billingService billing.IService,
	packageService packages.IService,
	limitService limits.IService,
	transactions postgresql.ITransactionManager,
	announcer Announcer,
	now Clock,
) *Packages {
	return &Packages{
		authorizer:   authorizer{access: accessService},
		log:          log,
		billing:      billingService,
		packages:     packageService,
		limits:       limitService,
		periods:      &periods{access: accessService, packages: packageService, announcer: announcer},
		transactions: transactions,
		announcer:    announcer,
		now:          now,
	}
}

func (h *Packages) Catalog(ctx context.Context) ([]dto.Package, error) {
	list, err := h.packages.ListCatalog(ctx)
	if err != nil {
		return nil, err
	}

	return dto.ToPackages(list), nil
}

func (h *Packages) Overview(ctx context.Context, req *packages.OverviewRequest) (*dto.Overview, error) {
	projectID := uuid.MustParse(req.ProjectID)
	membership, err := h.project(ctx, projectID, authz.MailRead)
	if err != nil {
		return nil, err
	}

	return h.overview(ctx, projectID, membership.TeamID)
}

// Choose starts, upgrades, schedules or undoes a Project's Package. Every
// choice of a Package follows the Team's Billing status like creating a
// Droplet does; a cancellation spends nothing, so it is always allowed.
func (h *Packages) Choose(ctx context.Context, req *packages.ChooseRequest) (*dto.Overview, error) {
	projectID := uuid.MustParse(req.ProjectID)
	membership, err := h.project(ctx, projectID, authz.MailWrite)
	if err != nil {
		return nil, err
	}

	var target *packages.Package
	if req.Code != packages.NoneCode {
		if target, err = h.packages.FindCatalog(ctx, req.Code); err != nil {
			return nil, err
		}
	}

	// The overview after the commit must not run on the committed
	// transaction, so the transaction keeps its own context.
	var out announcements
	txCtx, err := h.transactions.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = h.transactions.Rollback(txCtx) }()

	if err := h.choose(txCtx, projectID, membership.TeamID, target, &out); err != nil {
		return nil, err
	}
	if err := h.transactions.Commit(txCtx); err != nil {
		return nil, err
	}
	out.publish(ctx, h.log)

	return h.overview(ctx, projectID, membership.TeamID)
}

func (h *Packages) choose(ctx context.Context, projectID, teamID uuid.UUID, target *packages.Package, out *announcements) error {
	now := h.now()
	pp, _, err := h.periods.ensure(ctx, projectID, now, out)
	if err != nil {
		return err
	}
	if pp == nil {
		pp = &packages.ProjectPackage{ProjectID: projectID, TeamID: teamID}
	}

	kind := packages.Decide(pp.Current, pp.Scheduled, target)
	if kind == packages.ChangeNothing {
		return nil
	}
	if target != nil {
		if err := h.requireActiveBilling(ctx, teamID); err != nil {
			return err
		}
	}

	switch kind {
	case packages.ChangeStart, packages.ChangeUpgrade:
		return h.hold(ctx, pp, target, now, out)
	case packages.ChangeScheduled:
		pp.Scheduled = packages.Schedule(target)
	case packages.ChangeUnscheduled:
		pp.Scheduled = nil
	}

	if err := h.packages.SaveProject(ctx, pp); err != nil {
		return err
	}
	out.add(func(ctx context.Context) error { return h.announcer.SendingChanged(ctx, teamID, projectID, now) })

	return nil
}

// hold puts the Project on target at once: a first Package opens the Period,
// an upgrade raises its allowance and keeps what was already sent counted.
// Billing sees the old interval stop and the new one start at the same
// instant.
func (h *Packages) hold(ctx context.Context, pp *packages.ProjectPackage, target *packages.Package, now time.Time, out *announcements) error {
	project, err := h.access.FindProject(ctx, pp.ProjectID)
	if err != nil {
		return err
	}

	old := pp.Current
	pp.Current = packages.Hold(target, now)
	pp.Scheduled = nil
	if err := h.packages.SaveProject(ctx, pp); err != nil {
		return err
	}
	if err := h.packages.UpsertPeriod(ctx, &packages.Period{ProjectID: pp.ProjectID, Start: packages.PeriodStart(now), Allowance: target.MonthlyEmails}); err != nil {
		return err
	}
	if _, err := h.limits.Ensure(ctx, pp.ProjectID, pp.TeamID); err != nil {
		return err
	}

	if old != nil {
		out.add(func(ctx context.Context) error {
			return h.announcer.PackageStopped(ctx, pp.TeamID, pp.ProjectID, old.StartedAt, now)
		})
	}
	held := *pp.Current
	out.add(func(ctx context.Context) error { return h.announcer.PackageStarted(ctx, project, &held) })
	out.add(func(ctx context.Context) error { return h.announcer.SendingChanged(ctx, pp.TeamID, pp.ProjectID, now) })

	return nil
}

func (h *Packages) requireActiveBilling(ctx context.Context, teamID uuid.UUID) error {
	status, err := h.billing.StatusOf(ctx, teamID)
	if err != nil {
		return err
	}

	switch status {
	case billing.StatusActive:
		return nil
	case billing.StatusSuspended:
		return billing.ErrorTeamSuspended
	default:
		return billing.ErrorPaymentMethodRequired
	}
}

func (h *Packages) overview(ctx context.Context, projectID, teamID uuid.UUID) (*dto.Overview, error) {
	now := h.now()
	status, err := h.billing.StatusOf(ctx, teamID)
	if err != nil {
		return nil, err
	}

	pp, period, err := h.currentPeriod(ctx, projectID, now)
	if err != nil {
		return nil, err
	}

	lim, err := h.limits.Find(ctx, projectID)
	if err != nil {
		return nil, err
	}
	dailyLimit := limits.InitialDailyLimit
	if lim != nil {
		dailyLimit = lim.DailyLimit
	}

	today := limits.DayOf(now)
	day, err := h.limits.FindDay(ctx, projectID, today)
	if err != nil {
		return nil, err
	}
	usedToday := 0
	if day != nil {
		usedToday = day.Sent
		dailyLimit = max(dailyLimit, day.Limit)
	}

	window, err := h.limits.Window(ctx, projectID, today.AddDate(0, 0, 1-limits.WindowDays), today)
	if err != nil {
		return nil, err
	}

	out := &dto.Overview{
		ProjectID:     projectID,
		BillingStatus: string(status),
		DailyLimit:    dto.DailyLimit{Limit: dailyLimit, Used: usedToday, ResetsAt: today.AddDate(0, 0, 1)},
		Rates:         dto.Rates{WindowDays: limits.WindowDays, Sent: window.Sent, BounceRate: window.BounceRate(), ComplaintRate: window.ComplaintRate()},
	}
	if pp != nil && pp.Current != nil {
		out.Package = &dto.HeldPackage{
			Code: pp.Current.Code, MonthlyEmails: pp.Current.MonthlyEmails,
			MonthlyPrice: pp.Current.MonthlyPrice.StringFixed(packages.PriceScale), StartedAt: pp.Current.StartedAt,
		}
	}
	if pp != nil && pp.Scheduled != nil {
		out.ScheduledChange = &dto.ScheduledChange{Code: pp.Scheduled.Code, EffectiveAt: packages.NextPeriodStart(now)}
	}
	if period != nil {
		out.Period = &dto.Period{Start: period.Start, End: period.End(), Allowance: period.Allowance, Used: period.Used}
	}
	if lim != nil && lim.Paused() {
		out.Pause = &dto.Pause{Reason: string(*lim.PauseReason), PausedAt: *lim.PausedAt}
	}
	out.SendingState = sendingState(status, pp, period, lim, usedToday >= dailyLimit)

	return out, nil
}

// currentPeriod carries the Project into this Period first when the month
// turned before the rollover job ran.
func (h *Packages) currentPeriod(ctx context.Context, projectID uuid.UUID, now time.Time) (*packages.ProjectPackage, *packages.Period, error) {
	var out announcements
	txCtx, err := h.transactions.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = h.transactions.Rollback(txCtx) }()

	pp, period, err := h.periods.ensure(txCtx, projectID, now, &out)
	if err != nil {
		return nil, nil, err
	}
	if err := h.transactions.Commit(txCtx); err != nil {
		return nil, nil, err
	}
	out.publish(ctx, h.log)

	return pp, period, nil
}

// sendingState is the first refusal a send would meet, in the send path's
// order.
func sendingState(status billing.Status, pp *packages.ProjectPackage, period *packages.Period, lim *limits.Limits, dailyLimitReached bool) string {
	switch {
	case status == billing.StatusSuspended:
		return stateSuspended
	case pp == nil || pp.Current == nil:
		return stateNoPackage
	case lim != nil && lim.Paused():
		return statePaused
	case period != nil && period.Exhausted():
		return stateExhausted
	case dailyLimitReached:
		return stateDailyLimit
	default:
		return stateActive
	}
}
