package handlers

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/droplet/aggregates/billing"
	"github.com/piggystats/nodera/contexts/droplet/aggregates/droplet"
	"github.com/piggystats/nodera/contexts/droplet/aggregates/provider"
	"github.com/piggystats/nodera/contexts/droplet/providers"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/logger"
)

const (
	reasonUnpaid = "unpaid"
)

// TeamBilling applies what the billing context says about a Team
// (docs/adr/0006): it keeps the Billing status read model and acts on it. A
// suspension powers off the Team's running Droplets; the recovery after one
// powers on exactly those; a deletion request releases every Droplet the way
// a customer's delete would. Every reaction is idempotent, so a redelivered
// event, or one retried after a Provider failure, finishes the job rather
// than doing it twice.
type TeamBilling struct {
	*dropletOps
	billing billing.IService
}

func NewTeamBilling(
	log logger.ILog,
	billingService billing.IService,
	droplets droplet.IService,
	providerService provider.IService,
	registry ProviderRegistry,
	transactions postgresql.ITransactionManager,
	meter *Meter,
) *TeamBilling {
	return &TeamBilling{
		dropletOps: &dropletOps{log: log, droplets: droplets, providers: providerService, registry: registry, transactions: transactions, meter: meter},
		billing:    billingService,
	}
}

// StatusChanged is last-writer-wins on the event's own time: an older event
// arriving late changes nothing and triggers nothing.
func (h *TeamBilling) StatusChanged(ctx context.Context, teamID uuid.UUID, status billing.Status, changedAt time.Time) error {
	applied, err := h.billing.Upsert(ctx, &billing.TeamBilling{TeamID: teamID, Status: status, ChangedAt: changedAt})
	if err != nil {
		return err
	}
	if !applied {
		return nil
	}

	// Losing the last card only refuses new Droplets; the running ones are
	// the Dunning schedule's business, through suspension.
	switch status {
	case billing.StatusSuspended:
		return h.suspend(ctx, teamID)
	case billing.StatusActive:
		return h.resume(ctx, teamID)
	default:
		return nil
	}
}

// DeletionRequested releases every Droplet of the Team that still exists;
// the ones already on their way out are left to the poller.
func (h *TeamBilling) DeletionRequested(ctx context.Context, teamID uuid.UUID) error {
	droplets, err := h.droplets.FindByTeamID(ctx, teamID)
	if err != nil {
		return err
	}

	var firstErr error
	for _, d := range droplets {
		if !d.Status.AllowsDelete() {
			continue
		}
		if _, err := h.release(ctx, d.ID, reasonUnpaid); err != nil && !errors.Is(err, droplet.ErrorActionNotAllowed) {
			h.log.ErrorWithFields(err, logger.NewFields().Add("droplet", d.ID.String()).Add("team", teamID.String()))
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	return firstErr
}

// suspend powers off every running, idle Droplet of the Team and marks each
// one as powered off by this context, so the recovery knows which to power
// on. A Droplet still being ordered or busy with an action is powered off by
// the poller once it settles; one the customer had already powered off keeps
// no mark and stays off.
func (h *TeamBilling) suspend(ctx context.Context, teamID uuid.UUID) error {
	droplets, err := h.droplets.FindByTeamID(ctx, teamID)
	if err != nil {
		return err
	}

	var firstErr error
	for _, d := range droplets {
		if d.Status != droplet.StatusActive {
			continue
		}
		if err := h.powerOffForSuspension(ctx, d.ID); err != nil {
			h.log.ErrorWithFields(err, logger.NewFields().Add("droplet", d.ID.String()).Add("team", teamID.String()))
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	return firstErr
}

func (h *TeamBilling) powerOffForSuspension(ctx context.Context, id uuid.UUID) error {
	var target *droplet.Droplet
	err := h.update(ctx, id, func(ctx context.Context, current *droplet.Droplet) error {
		// Not running any more, or an action of its own in flight: the poller
		// settles it and a retry of this event looks again.
		if current.Status != droplet.StatusActive || current.PendingAction != nil || current.ProviderRef == nil {
			return nil
		}
		action := droplet.ActionPowerOff
		at := droplet.Now()
		current.PendingAction = &action
		current.SuspendedPowerOffAt = &at
		target = current

		return h.droplets.Save(ctx, current)
	})
	if err != nil || target == nil {
		return err
	}

	if err := h.powerAction(ctx, target, droplet.ActionPowerOff); err != nil {
		h.unmarkSuspension(ctx, target.ID)

		return err
	}

	return nil
}

// resume powers on the Droplets this context powered off for the suspension
// and clears their mark; a Droplet the customer powered off is not touched,
// and one whose power-off is still in flight is left to the poller.
func (h *TeamBilling) resume(ctx context.Context, teamID uuid.UUID) error {
	droplets, err := h.droplets.FindByTeamID(ctx, teamID)
	if err != nil {
		return err
	}

	var firstErr error
	for _, d := range droplets {
		if d.SuspendedPowerOffAt == nil {
			continue
		}
		if err := h.powerOnAfterSuspension(ctx, d.ID); err != nil {
			h.log.ErrorWithFields(err, logger.NewFields().Add("droplet", d.ID.String()).Add("team", teamID.String()))
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	return firstErr
}

func (h *TeamBilling) powerOnAfterSuspension(ctx context.Context, id uuid.UUID) error {
	var (
		target   *droplet.Droplet
		markedAt *time.Time
	)
	err := h.update(ctx, id, func(ctx context.Context, current *droplet.Droplet) error {
		// Still being powered off: the mark stays and the poller powers it on
		// once the power-off lands.
		if current.SuspendedPowerOffAt == nil || current.PendingAction != nil {
			return nil
		}
		markedAt = current.SuspendedPowerOffAt
		current.SuspendedPowerOffAt = nil
		// A Droplet that failed or is being deleted meanwhile only loses its mark.
		if current.Status == droplet.StatusOff && current.ProviderRef != nil {
			action := droplet.ActionPowerOn
			current.PendingAction = &action
			target = current
		}

		return h.droplets.Save(ctx, current)
	})
	if err != nil || target == nil {
		return err
	}

	if err := h.powerAction(ctx, target, droplet.ActionPowerOn); err != nil {
		// The mark comes back so a retry of the event powers it on after all.
		h.restoreSuspension(ctx, target.ID, markedAt)

		return err
	}

	return nil
}

func (h *TeamBilling) powerAction(ctx context.Context, d *droplet.Droplet, action droplet.Action) error {
	adapter, err := h.adapterFor(ctx, d)
	if err != nil {
		return err
	}

	return adapter.PowerAction(ctx, *d.ProviderRef, providers.Action(action))
}

func (h *TeamBilling) unmarkSuspension(ctx context.Context, id uuid.UUID) {
	err := h.update(ctx, id, func(ctx context.Context, current *droplet.Droplet) error {
		if current.PendingAction != nil && *current.PendingAction == droplet.ActionPowerOff {
			current.PendingAction = nil
		}
		current.SuspendedPowerOffAt = nil

		return h.droplets.Save(ctx, current)
	})
	if err != nil {
		h.log.ErrorWithFields(err, logger.NewFields().Add("droplet", id.String()))
	}
}

func (h *TeamBilling) restoreSuspension(ctx context.Context, id uuid.UUID, markedAt *time.Time) {
	err := h.update(ctx, id, func(ctx context.Context, current *droplet.Droplet) error {
		if current.PendingAction != nil && *current.PendingAction == droplet.ActionPowerOn {
			current.PendingAction = nil
		}
		current.SuspendedPowerOffAt = markedAt

		return h.droplets.Save(ctx, current)
	})
	if err != nil {
		h.log.ErrorWithFields(err, logger.NewFields().Add("droplet", id.String()))
	}
}
