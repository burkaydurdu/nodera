package handlers

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/droplet/aggregates/droplet"
	"github.com/piggystats/nodera/contexts/droplet/aggregates/provider"
	"github.com/piggystats/nodera/contexts/droplet/providers"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/logger"
)

const (
	reasonNoServer = "no server to release"
)

// dropletOps is what every change to a Droplet goes through, whether a
// customer, the billing context or a replay asked for it: one locked
// read-modify-write that also claims the meter's boundaries in the same
// transaction and announces them after the commit.
type dropletOps struct {
	log          logger.ILog
	droplets     droplet.IService
	providers    provider.IService
	registry     ProviderRegistry
	transactions postgresql.ITransactionManager
	meter        *Meter
}

// update locks the row for the transaction, lets mutate see the current row
// and persist what it decides, and the commit releases the lock. The poller
// does the same, so neither side can overwrite the other's change.
func (o *dropletOps) update(ctx context.Context, id uuid.UUID, mutate func(ctx context.Context, current *droplet.Droplet) error) error {
	txCtx, err := o.transactions.Begin(ctx)
	if err != nil {
		return err
	}

	current, err := o.droplets.FindByIDForUpdate(txCtx, id)
	if err != nil {
		_ = o.transactions.Rollback(txCtx)

		return err
	}
	if err := mutate(txCtx, current); err != nil {
		_ = o.transactions.Rollback(txCtx)

		return err
	}

	claimed := current.ClaimBillingEvents(droplet.Now())
	if len(claimed) > 0 {
		if err := o.droplets.Save(txCtx, current); err != nil {
			_ = o.transactions.Rollback(txCtx)

			return err
		}
	}

	if err := o.transactions.Commit(txCtx); err != nil {
		return err
	}
	o.meter.Publish(ctx, current, claimed)

	return nil
}

// release stops the meter at once and asks the Provider to release the
// server; the poller (or the absence of a server) finishes the job. Allowed
// from every status but deleting and deleted: a customer may abandon a
// Droplet still being ordered. Never a hard delete.
func (o *dropletOps) release(ctx context.Context, id uuid.UUID, reason string) (*droplet.Droplet, error) {
	var d *droplet.Droplet
	err := o.update(ctx, id, func(ctx context.Context, current *droplet.Droplet) error {
		if !current.Status.AllowsDelete() {
			return droplet.ErrorActionNotAllowed
		}
		if err := o.droplets.Transition(ctx, current, droplet.StatusDeleting, reason); err != nil {
			return err
		}
		d = current
		if current.ProviderRef != nil {
			return nil
		}

		return o.droplets.Transition(ctx, current, droplet.StatusDeleted, reasonNoServer)
	})
	if err != nil {
		return nil, err
	}
	if d.ProviderRef == nil {
		return d, nil
	}

	adapter, err := o.adapterFor(ctx, d)
	if err != nil {
		return nil, err
	}
	if err := adapter.DeleteServer(ctx, *d.ProviderRef); err != nil && !errors.Is(err, provider.ErrorServerNotFound) {
		// The Droplet stays deleting; the poller asks the Provider again on
		// every run until the server is gone.
		o.log.ErrorWithFields(err, logger.NewFields().Add("droplet", d.ID.String()))
	}

	return d, nil
}

// clearPending takes back the marker of a request the Provider never took;
// only that request's own marker, should the poller have moved on already.
func (o *dropletOps) clearPending(ctx context.Context, id uuid.UUID, action droplet.Action) {
	err := o.update(ctx, id, func(ctx context.Context, current *droplet.Droplet) error {
		if current.PendingAction == nil || *current.PendingAction != action {
			return nil
		}
		current.PendingAction = nil

		return o.droplets.Save(ctx, current)
	})
	if err != nil {
		o.log.ErrorWithFields(err, logger.NewFields().Add("droplet", id.String()).Add("action", string(action)))
	}
}

func (o *dropletOps) adapterFor(ctx context.Context, d *droplet.Droplet) (providers.Provider, error) {
	p, err := o.providers.FindByID(ctx, d.ProviderID)
	if err != nil {
		return nil, err
	}

	return o.registry.Get(p.Code)
}
