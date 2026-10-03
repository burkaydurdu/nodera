package jobs

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/droplet/aggregates/billing"
	"github.com/piggystats/nodera/contexts/droplet/aggregates/droplet"
	"github.com/piggystats/nodera/contexts/droplet/aggregates/provider"
	"github.com/piggystats/nodera/contexts/droplet/providers"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/utils/str"
)

const (
	reasonProviderReported = "provider reported "
	reasonReleased         = "released without a server"
	reasonGoneAtProvider   = "server no longer exists at the provider"
	reasonSuspended        = "suspended"
)

// PollDroplets asks the Provider about every Droplet in a transitional status
// or with a pending power action, and applies what it says. Polling is the
// only status source; there are no webhooks. The list is a snapshot: each
// Droplet is re-read under a row lock before anything is decided about it,
// so a user action landing in between is never overwritten. A poll that
// makes a Droplet cross a meter boundary claims it in the same transaction
// and announces it after the commit, so a redelivered poll announces nothing.
func (j *Jobs) PollDroplets(ctx context.Context) error {
	transitional, err := j.droplets.FindTransitional(ctx)
	if err != nil {
		return err
	}

	var firstErr error
	for _, d := range transitional {
		if err := j.pollOne(ctx, d.ID); err != nil {
			j.log.ErrorWithFields(err, logger.NewFields().Add("droplet", d.ID.String()).Add("status", string(d.Status)))
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	return firstErr
}

func (j *Jobs) pollOne(ctx context.Context, id uuid.UUID) error {
	txCtx, err := j.transactions.Begin(ctx)
	if err != nil {
		return err
	}

	d, err := j.droplets.FindByIDForUpdate(txCtx, id)
	if err != nil {
		_ = j.transactions.Rollback(txCtx)

		return err
	}
	from := d.Status
	if err := j.applyProviderState(txCtx, d); err != nil {
		_ = j.transactions.Rollback(txCtx)

		return err
	}
	if err := j.applyBillingStatus(txCtx, d); err != nil {
		_ = j.transactions.Rollback(txCtx)

		return err
	}

	claimed := d.ClaimBillingEvents(j.now())
	if len(claimed) > 0 {
		if err := j.droplets.Save(txCtx, d); err != nil {
			_ = j.transactions.Rollback(txCtx)

			return err
		}
	}

	if err := j.transactions.Commit(txCtx); err != nil {
		return err
	}
	j.meter.Publish(ctx, d, claimed)
	j.status.Announce(ctx, d, &from)

	return nil
}

// applyProviderState is one poller step on a locked Droplet.
func (j *Jobs) applyProviderState(ctx context.Context, d *droplet.Droplet) error {
	timedOut := d.Status == droplet.StatusProvisioning && j.now().Sub(d.CreatedAt) > ProvisioningTimeout

	// No supplier reference: nothing to ask. A provisioning Droplet without
	// one is still being ordered, unless it has waited too long.
	if d.ProviderRef == nil {
		switch {
		case d.Status == droplet.StatusDeleting:
			return j.droplets.Transition(ctx, d, droplet.StatusDeleted, reasonReleased)
		case timedOut:
			return j.droplets.Fail(ctx, d, droplet.FailureTimedOut, droplet.ErrorProvisioningTimedOut.Message)
		default:
			return nil
		}
	}

	adapter, err := j.adapterFor(ctx, d)
	if err != nil {
		return err
	}

	switch {
	case d.Status == droplet.StatusDeleting:
		return j.release(ctx, adapter, d)
	case timedOut:
		return j.timeOut(ctx, adapter, d)
	}

	server, err := adapter.GetServer(ctx, *d.ProviderRef)
	if err != nil {
		if errors.Is(err, provider.ErrorServerNotFound) {
			return j.serverGone(ctx, d)
		}

		return err
	}

	reported := droplet.Status(server.Status)
	changed := applyAddresses(d, server.IPv4, server.IPv6)

	switch {
	case reported == droplet.StatusFailed && d.Status.CanTransitionTo(reported):
		return j.droplets.Fail(ctx, d, droplet.FailureProviderFailed, transitionReason(d, reported))
	case reported != d.Status && d.Status.CanTransitionTo(reported):
		return j.droplets.Transition(ctx, d, reported, transitionReason(d, reported))
	case d.PendingAction != nil && reported == d.PendingAction.TargetStatus():
		// A reboot lands where it started; the pending marker is what clears.
		d.PendingAction = nil

		return j.droplets.Save(ctx, d)
	case changed:
		return j.droplets.Save(ctx, d)
	default:
		return nil
	}
}

// release asks the Provider to delete the server again on every poll: the
// request the handler made may have failed, and deleting is idempotent. The
// Droplet is deleted once the Provider no longer has the server or reports
// it deleted; otherwise it stays deleting for the next poll.
func (j *Jobs) release(ctx context.Context, adapter adapter, d *droplet.Droplet) error {
	if err := adapter.DeleteServer(ctx, *d.ProviderRef); err != nil {
		if errors.Is(err, provider.ErrorServerNotFound) {
			return j.droplets.Transition(ctx, d, droplet.StatusDeleted, reasonGoneAtProvider)
		}

		return err
	}

	server, err := adapter.GetServer(ctx, *d.ProviderRef)
	if err != nil {
		if errors.Is(err, provider.ErrorServerNotFound) {
			return j.droplets.Transition(ctx, d, droplet.StatusDeleted, reasonGoneAtProvider)
		}

		return err
	}
	if droplet.Status(server.Status) == droplet.StatusDeleted {
		return j.droplets.Transition(ctx, d, droplet.StatusDeleted, reasonProviderReported+string(droplet.StatusDeleted))
	}

	return nil
}

// timeOut fails a Droplet the Provider did not deliver in time and releases
// whatever server it did create, best-effort: Nodera must not keep paying
// for it, but the customer's outcome does not wait on the Provider.
func (j *Jobs) timeOut(ctx context.Context, adapter adapter, d *droplet.Droplet) error {
	fields := logger.NewFields().Add("droplet", d.ID.String()).Add("provider_ref", *d.ProviderRef)
	switch err := adapter.DeleteServer(ctx, *d.ProviderRef); {
	case err == nil:
		j.log.InfoWithFields("provisioning timed out: server released", fields)
	case errors.Is(err, provider.ErrorServerNotFound):
		j.log.InfoWithFields("provisioning timed out: no server to release", fields)
	default:
		j.log.ErrorWithFields(err, fields.Add("outcome", "provisioning timed out: server could not be released"))
	}

	return j.droplets.Fail(ctx, d, droplet.FailureTimedOut, droplet.ErrorProvisioningTimedOut.Message)
}

// serverGone: a live Droplet whose server vanished is failed; one still
// provisioning gets another poll until the timeout decides.
func (j *Jobs) serverGone(ctx context.Context, d *droplet.Droplet) error {
	if d.Status != droplet.StatusProvisioning && d.Status.CanTransitionTo(droplet.StatusFailed) {
		return j.droplets.Fail(ctx, d, droplet.FailureServerGone, reasonGoneAtProvider)
	}

	return nil
}

// applyBillingStatus brings a Droplet that has just settled in line with its
// Team's Billing status. The suspension and recovery events act on the
// Droplets that are idle when they arrive; one still being ordered or busy
// with an action is caught here, on the poll where it settles. A suspended
// Team's running Droplet is powered off and marked; a marked Droplet that is
// off again while its Team is active is powered back on. The Provider is
// asked inside the transaction, so a refusal rolls the whole poll step back
// and the next poll tries again.
func (j *Jobs) applyBillingStatus(ctx context.Context, d *droplet.Droplet) error {
	if d.ProviderRef == nil || d.PendingAction != nil {
		return nil
	}

	var want billing.Status
	switch {
	case d.Status == droplet.StatusActive && d.SuspendedPowerOffAt == nil:
		want = billing.StatusSuspended
	case d.Status == droplet.StatusOff && d.SuspendedPowerOffAt != nil:
		want = billing.StatusActive
	default:
		return nil
	}

	status, err := j.billing.StatusOf(ctx, d.TeamID)
	if err != nil || status != want {
		return err
	}

	action := droplet.ActionPowerOn
	d.SuspendedPowerOffAt = nil
	if want == billing.StatusSuspended {
		action = droplet.ActionPowerOff
		at := droplet.Now()
		d.SuspendedPowerOffAt = &at
	}
	d.PendingAction = &action
	if err := j.droplets.Save(ctx, d); err != nil {
		return err
	}

	adapter, err := j.adapterFor(ctx, d)
	if err != nil {
		return err
	}

	return adapter.PowerAction(ctx, *d.ProviderRef, providers.Action(action))
}

// transitionReason names who asked for what the Provider now reports: a
// power-off this context requested for a suspended Team is recorded as such,
// so the history tells the customer why their server went down.
func transitionReason(d *droplet.Droplet, reported droplet.Status) string {
	if reported == droplet.StatusOff && d.SuspendedPowerOffAt != nil && d.PendingAction != nil && *d.PendingAction == droplet.ActionPowerOff {
		return reasonSuspended
	}

	return reasonProviderReported + string(reported)
}

func applyAddresses(d *droplet.Droplet, ipv4, ipv6 string) bool {
	changed := false
	if str.IsNotEmpty(ipv4) && (d.IPv4 == nil || *d.IPv4 != ipv4) {
		v := ipv4
		d.IPv4 = &v
		changed = true
	}
	if str.IsNotEmpty(ipv6) && (d.IPv6 == nil || *d.IPv6 != ipv6) {
		v := ipv6
		d.IPv6 = &v
		changed = true
	}

	return changed
}

func (j *Jobs) adapterFor(ctx context.Context, d *droplet.Droplet) (adapter, error) {
	p, err := j.providers.FindByID(ctx, d.ProviderID)
	if err != nil {
		return nil, err
	}

	return j.registry.Get(p.Code)
}
