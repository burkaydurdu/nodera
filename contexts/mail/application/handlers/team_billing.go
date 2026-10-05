package handlers

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/mail/aggregates/access"
	"github.com/piggystats/nodera/contexts/mail/aggregates/billing"
	"github.com/piggystats/nodera/contexts/mail/aggregates/packages"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/logger"
)

// TeamBilling reacts to the billing context's events: a suspended Team's
// sending stops at the door (the send path reads the status), and on day 21
// of Dunning the Team's Packages end so no new Period is charged, while its
// Sending domains, API keys and Suppressions stay.
type TeamBilling struct {
	log          logger.ILog
	billing      billing.IService
	packages     packages.IService
	periods      *periods
	transactions postgresql.ITransactionManager
	announcer    Announcer
}

func NewTeamBilling(
	log logger.ILog,
	accessService access.IService,
	billingService billing.IService,
	packageService packages.IService,
	transactions postgresql.ITransactionManager,
	announcer Announcer,
) *TeamBilling {
	return &TeamBilling{
		log: log, billing: billingService, packages: packageService,
		periods:      &periods{access: accessService, packages: packageService, announcer: announcer},
		transactions: transactions, announcer: announcer,
	}
}

func (h *TeamBilling) StatusChanged(ctx context.Context, teamID uuid.UUID, status billing.Status, changedAt time.Time) error {
	applied, err := h.billing.Upsert(ctx, &billing.TeamBilling{TeamID: teamID, Status: status, ChangedAt: changedAt})
	if err != nil || !applied {
		return err
	}

	held, err := h.packages.ListHeldByTeam(ctx, teamID)
	if err != nil {
		return err
	}
	var out announcements
	for _, pp := range held {
		projectID := pp.ProjectID
		out.add(func(ctx context.Context) error { return h.announcer.SendingChanged(ctx, teamID, projectID, changedAt) })
	}
	out.publish(ctx, h.log)

	return nil
}

// DeletionRequested ends each Package where the request finds it. A change
// its Period boundary already owed applies first, so a cancellation due at
// the month's first instant never leaves the new month charged because the
// event came before the rollover job.
func (h *TeamBilling) DeletionRequested(ctx context.Context, teamID uuid.UUID, requestedAt time.Time) error {
	var out announcements
	txCtx, err := h.transactions.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = h.transactions.Rollback(txCtx) }()

	held, err := h.packages.ListHeldByTeam(txCtx, teamID)
	if err != nil {
		return err
	}
	for _, listed := range held {
		pp, _, err := h.periods.ensure(txCtx, listed.ProjectID, requestedAt, &out)
		if err != nil {
			return err
		}
		if pp == nil || pp.Current == nil {
			continue
		}

		startedAt := pp.Current.StartedAt
		stoppedAt := requestedAt
		if stoppedAt.Before(startedAt) {
			stoppedAt = startedAt
		}
		pp.Current, pp.Scheduled = nil, nil
		if err := h.packages.SaveProject(txCtx, pp); err != nil {
			return err
		}
		projectID := pp.ProjectID
		out.add(func(ctx context.Context) error {
			return h.announcer.PackageStopped(ctx, teamID, projectID, startedAt, stoppedAt)
		})
		out.add(func(ctx context.Context) error { return h.announcer.SendingChanged(ctx, teamID, projectID, stoppedAt) })
	}

	if err := h.transactions.Commit(txCtx); err != nil {
		return err
	}
	out.publish(ctx, h.log)

	return nil
}
