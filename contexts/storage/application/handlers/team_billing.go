package handlers

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/storage/aggregates/billing"
	"github.com/piggystats/nodera/contexts/storage/aggregates/storage"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/logger"
)

// TeamBilling reacts to the billing context's events: a suspended Team's
// Storages stop being read and written while their files stay, and on day 21
// of Dunning the Team's Storages are deleted with its Droplets.
type TeamBilling struct {
	log          logger.ILog
	billing      billing.IService
	storages     storage.IService
	deleter      *Storages
	sync         *syncer
	transactions postgresql.ITransactionManager
	announcer    Announcer
}

func NewTeamBilling(log logger.ILog, billingService billing.IService, storageService storage.IService, storages *Storages, sync *syncer, transactions postgresql.ITransactionManager, announcer Announcer) *TeamBilling {
	return &TeamBilling{log: log, billing: billingService, storages: storageService, deleter: storages, sync: sync, transactions: transactions, announcer: announcer}
}

func (h *TeamBilling) StatusChanged(ctx context.Context, teamID uuid.UUID, status billing.Status, changedAt time.Time) error {
	applied, err := h.billing.Upsert(ctx, &billing.TeamBilling{TeamID: teamID, Status: status, ChangedAt: changedAt})
	if err != nil || !applied {
		return err
	}

	changed, err := h.storages.SetTeamSuspended(ctx, teamID, status == billing.StatusSuspended)
	if err != nil {
		return err
	}
	var out announcements
	for _, id := range changed {
		s, err := h.storages.FindByID(ctx, id)
		if err != nil {
			return err
		}
		out.add(func(ctx context.Context) error {
			return h.announcer.StorageChanged(ctx, teamID, s.ProjectID, s.ID, changedAt)
		})
	}
	out.publish(ctx, h.log)
	for _, id := range changed {
		h.sync.follow(ctx, id)
	}

	return nil
}

// DeletionRequested deletes every open Storage of the Team, each Package
// ending at the event's time.
func (h *TeamBilling) DeletionRequested(ctx context.Context, teamID uuid.UUID, requestedAt time.Time) error {
	var out announcements
	var stopped []*storage.Storage
	txCtx, err := h.transactions.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = h.transactions.Rollback(txCtx) }()

	list, err := h.storages.ListActiveByTeam(txCtx, teamID)
	if err != nil {
		return err
	}
	for _, listed := range list {
		s, err := h.storages.FindByIDForUpdate(txCtx, listed.ID)
		if err != nil {
			return err
		}
		if s.Status != storage.StatusActive {
			continue
		}
		if err := h.deleter.markDeleting(txCtx, s, requestedAt, &out); err != nil {
			return err
		}
		stopped = append(stopped, s)
	}

	if err := h.transactions.Commit(txCtx); err != nil {
		return err
	}
	out.publish(ctx, h.log)
	for _, s := range stopped {
		h.deleter.stopNow(ctx, s)
	}

	return nil
}
