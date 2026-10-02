package handlers

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/billing/aggregates/billingstatus"
	"github.com/piggystats/nodera/contexts/billing/aggregates/invoice"
	"github.com/piggystats/nodera/contexts/billing/aggregates/payment"
	"github.com/piggystats/nodera/contexts/billing/aggregates/paymentmethod"
	"github.com/piggystats/nodera/contexts/billing/aggregates/usage"
	"github.com/piggystats/nodera/contexts/billing/async/events"
	"github.com/piggystats/nodera/contexts/billing/providers"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/utils/str"
)

const (
	// lostAnswerAfter is when an attempt nobody answered is treated as lost:
	// twice a provider call's timeout, so a call still in flight is never
	// raced.
	lostAnswerAfter = 2 * providers.CallTimeout
	// uncollectedAfter is how long an issued Invoice may wait for its first
	// attempt before the sweep makes it.
	uncollectedAfter = 15 * time.Minute
)

// The methods below are what the worker's jobs run. Each walks its Teams or
// Invoices one by one: a failure is collected and the rest still run, so one
// broken Team never holds up everyone's billing.

// ClosePeriod issues the Period Invoice of every Team with usage in it. It is
// idempotent per Team and Period, so the worker may run it as often as it
// likes once the Period has ended.
func (c *Collection) ClosePeriod(ctx context.Context, period usage.Period, at time.Time) error {
	teams, err := c.Usage.TeamsWithUsage(ctx, period.Start, period.End)
	if err != nil {
		return err
	}

	var errs []error
	for _, teamID := range teams {
		if _, err := c.Issue(ctx, teamID, invoice.KindPeriod, period, period.End, at); err != nil {
			c.logFailure(err, "close period", logger.NewFields().Add("team", teamID.String()))
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// CheckThresholds issues a Threshold Invoice for every Team whose unbilled
// usage in the open Period reached its Threshold.
func (c *Collection) CheckThresholds(ctx context.Context, at time.Time) error {
	period := usage.PeriodOf(at)
	teams, err := c.Usage.TeamsWithUsage(ctx, period.Start, at)
	if err != nil {
		return err
	}

	var errs []error
	for _, teamID := range teams {
		if err := c.checkThreshold(ctx, teamID, period, at); err != nil {
			c.logFailure(err, "check threshold", logger.NewFields().Add("team", teamID.String()))
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func (c *Collection) checkThreshold(ctx context.Context, teamID uuid.UUID, period usage.Period, at time.Time) error {
	d, err := c.draft(ctx, teamID, period, at)
	if err != nil {
		return err
	}
	t, err := c.thresholdFor(ctx, teamID, at)
	if err != nil {
		return err
	}
	if !t.Crossed(d.Subtotal) {
		return nil
	}
	_, err = c.Issue(ctx, teamID, invoice.KindThreshold, period, at, at)

	return err
}

// RunDunning carries every payment_failed Invoice to the step its schedule
// has reached. A run that was missed catches up in one go: the latest retry
// due is made once, then the latest warning or the deletion request.
func (c *Collection) RunDunning(ctx context.Context, at time.Time) error {
	failed, err := c.Invoices.FindFailed(ctx)
	if err != nil {
		return err
	}

	var errs []error
	for _, inv := range failed {
		if err := c.dun(ctx, inv, at); err != nil {
			c.logFailure(err, "dunning", logger.NewFields().Add("invoice", inv.ID.String()))
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// dun runs the Invoice's due steps. From the day-7 step on, the schedule
// alone keeps the Team suspended: the status is settled after the step
// whatever its attempt answered, a provider that did not answer included.
// The day-7 retry settles after its attempt rather than before, so a retry
// that collects never suspends the Team for an instant.
func (c *Collection) dun(ctx context.Context, inv *invoice.Invoice, at time.Time) error {
	if inv.FailedAt == nil {
		return nil
	}
	due := invoice.DueStep(*inv.FailedAt, at)
	if due <= inv.DunningStep {
		if inv.DunningStep >= invoice.StepSuspend {
			return c.settle(ctx, inv.TeamID, at)
		}

		return nil
	}

	if inv.DunningStep < invoice.StepRetryDay7 {
		retry := min(due, invoice.StepRetryDay7)
		advanced, err := c.advance(ctx, inv, retry, false, at)
		if err != nil || !advanced {
			return err
		}
		if err := c.retry(ctx, inv, at); err != nil {
			return err
		}
		if retry >= invoice.StepSuspend {
			if err := c.settle(ctx, inv.TeamID, at); err != nil {
				return err
			}
		}
		if due <= invoice.StepRetryDay7 {
			return nil
		}
	}

	advanced, err := c.advance(ctx, inv, due, true, at)
	if err != nil || !advanced {
		return err
	}
	if due == invoice.StepDeleteDay21 {
		return c.requestDeletion(ctx, inv.TeamID, at)
	}

	recipients, teamName, err := c.recipients(ctx, inv.TeamID)
	if err != nil {
		return err
	}

	return c.Notifier.DropletsDeletionWarning(ctx, events.DropletsDeletionWarningBody{
		Recipients: recipients, TeamName: teamName, InvoiceNumber: inv.Number,
		DeletionAt: invoice.DeletionAt(*inv.FailedAt).UTC(), PayURL: c.payURL(inv.TeamID, inv.ID),
	})
}

// retry charges the Invoice again unless an attempt may still collect: a
// pending charge or an open confirmation link makes a second charge a risk,
// so that retry is skipped and the schedule moves on.
func (c *Collection) retry(ctx context.Context, inv *invoice.Invoice, at time.Time) error {
	_, err := c.charge(ctx, inv.ID, nil, payment.TriggerDunning, at)
	if errors.Is(err, invoice.ErrorNotPayable) {
		return nil
	}
	if errors.Is(err, invoice.ErrorPaymentInProgress) {
		c.Log.InfoWithFields("dunning retry skipped: an attempt is still in progress", logger.NewFields().Add("invoice", inv.ID.String()))

		return nil
	}

	return err
}

// requestDeletion asks the droplet context to delete the Team's Droplets,
// only while the Team is suspended: a Team someone reinstated keeps them.
func (c *Collection) requestDeletion(ctx context.Context, teamID uuid.UUID, at time.Time) error {
	st, err := c.Statuses.Find(ctx, teamID)
	if err != nil {
		return err
	}
	if st.Status != billingstatus.Suspended {
		c.Log.WarnWithFields("droplet deletion withheld: the team is not suspended", logger.NewFields().
			Add("team", teamID.String()).Add("status", string(st.Status)))

		return nil
	}

	return c.Deletions.Publish(ctx, teamID, at.UTC())
}

// advance records that the Invoice has reached a Dunning step before the
// step acts, so two runs never act twice; it answers false when the Invoice
// was paid or moved on meanwhile. With settle, the Team's status is derived
// in the same transaction, which suspends it from the day-7 step on.
func (c *Collection) advance(ctx context.Context, inv *invoice.Invoice, step int, settle bool, at time.Time) (bool, error) {
	var (
		advanced bool
		change   *statusChange
	)
	err := inTransaction(ctx, c.Transactions, func(ctx context.Context) error {
		if _, err := c.Statuses.FindForUpdate(ctx, inv.TeamID, at); err != nil {
			return err
		}
		current, err := c.Invoices.FindForUpdate(ctx, inv.ID)
		if err != nil {
			return err
		}
		if current.Status != invoice.StatusPaymentFailed || current.DunningStep >= step {
			return nil
		}
		current.DunningStep = step
		if err := c.Invoices.Save(ctx, current); err != nil {
			return err
		}
		inv.DunningStep, advanced = step, true
		if settle && step >= invoice.StepSuspend {
			change, err = c.settleStatus(ctx, inv.TeamID, at)
		}

		return err
	})
	if err != nil {
		return false, err
	}
	c.announceStatus(ctx, inv.TeamID, change)

	return advanced, nil
}

// Reconcile settles what the providers know and Nodera does not yet: every
// stored webhook names a Payment whose state is fetched from the provider's
// API rather than read from the body, and every attempt nobody answered is
// looked up by its key before it is ever charged again.
func (c *Collection) Reconcile(ctx context.Context, at time.Time) error {
	var errs []error

	stored, err := c.Payments.FindUnprocessedWebhookEvents(ctx, batchSize)
	if err != nil {
		return err
	}
	for _, e := range stored {
		if err := c.reconcileEvent(ctx, e, at); err != nil {
			c.logFailure(err, "reconcile webhook", logger.NewFields().Add("event", e.ProviderEventID))
			errs = append(errs, err)

			continue
		}
		if err := c.Payments.MarkWebhookEventProcessed(ctx, e); err != nil {
			errs = append(errs, err)
		}
	}

	pending, err := c.Payments.FindPending(ctx, at.Add(-lostAnswerAfter), batchSize)
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	for _, p := range pending {
		if err := c.reconcilePayment(ctx, p, at); err != nil {
			c.logFailure(err, "reconcile payment", logger.NewFields().Add("payment", p.ID.String()))
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// reconcileEvent answers nil for an event that needs nothing more, so it is
// marked processed; an error leaves it for the next run. A webhook may name
// a payment Nodera never heard the reference of (the charge's answer was
// lost): the provider's record names the attempt by its key.
func (c *Collection) reconcileEvent(ctx context.Context, e *payment.WebhookEvent, at time.Time) error {
	if e.Kind != string(providers.WebhookPayment) || str.IsBlank(e.ProviderPaymentRef) {
		return nil
	}
	p, err := c.Payments.FindByProviderRef(ctx, e.Provider, e.ProviderPaymentRef)
	if errors.Is(err, payment.ErrorNotFound) {
		return c.reconcileUnknownRef(ctx, e, at)
	}
	if err != nil || p.Settled() {
		return err
	}

	return c.reconcilePayment(ctx, p, at)
}

func (c *Collection) reconcileUnknownRef(ctx context.Context, e *payment.WebhookEvent, at time.Time) error {
	adapter, err := c.Registry.Get(e.Provider)
	if err != nil {
		return err
	}
	result, err := adapter.FetchPayment(ctx, e.ProviderPaymentRef)
	if errors.Is(err, providers.ErrUnknownRef) {
		c.Log.InfoWithFields("webhook names no known payment", logger.NewFields().Add("event", e.ProviderEventID))

		return nil
	}
	if err != nil {
		return err
	}
	if str.IsBlank(result.IdempotencyKey) {
		return nil
	}
	p, err := c.Payments.FindByIdempotencyKey(ctx, result.IdempotencyKey)
	if errors.Is(err, payment.ErrorNotFound) {
		c.Log.InfoWithFields("webhook names a payment of no known attempt", logger.NewFields().Add("event", e.ProviderEventID))

		return nil
	}
	if err != nil || p.Settled() {
		return err
	}
	_, err = c.applyOutcome(ctx, p.ID, result, at)

	return err
}

// reconcilePayment settles an attempt from the provider's records. One
// with a reference is fetched; one without (the charge's answer was lost) is
// looked up by its idempotency key, and charged again only when the
// provider holds nothing for it: Craftgate does not dedupe by key, so a
// blind retry could charge twice.
func (c *Collection) reconcilePayment(ctx context.Context, p *payment.Payment, at time.Time) error {
	if p.PaymentMethodID == nil {
		_, err := c.applyOutcome(ctx, p.ID, &providers.ChargeResult{State: providers.StateFailed, FailureReason: payment.ReasonNoPaymentMethod}, at)

		return err
	}
	adapter, err := c.Registry.Get(p.Provider)
	if err != nil {
		return err
	}

	var result *providers.ChargeResult
	if str.IsNotEmpty(p.ProviderRef) {
		result, err = adapter.FetchPayment(ctx, p.ProviderRef)
	} else {
		result, err = adapter.FindCharge(ctx, p.IdempotencyKey)
		if errors.Is(err, providers.ErrUnknownRef) {
			result, err = c.recharge(ctx, p)
		}
	}
	if reason, declined := providers.Declined(err); declined {
		result, err = &providers.ChargeResult{State: providers.StateFailed, FailureReason: reason}, nil
	}
	if err != nil {
		return err
	}
	_, err = c.applyOutcome(ctx, p.ID, result, at)

	return err
}

func (c *Collection) recharge(ctx context.Context, p *payment.Payment) (*providers.ChargeResult, error) {
	method, err := c.PaymentMethods.FindAnyByID(ctx, p.TeamID, *p.PaymentMethodID)
	if err != nil {
		return nil, err
	}
	inv, err := c.Invoices.FindAnyByID(ctx, p.InvoiceID)
	if err != nil {
		return nil, err
	}

	return c.callCharge(ctx, p, method, inv.Number)
}

// Sweep collects what fell between the steps: a confirmation link that
// lapsed fails its attempt, which starts Dunning; an Invoice still issued
// with no attempt in progress since uncollectedAfter is charged, its issue
// email sent first if it never went out.
func (c *Collection) Sweep(ctx context.Context, at time.Time) error {
	var errs []error

	lapsed, err := c.Payments.FindExpiredConfirmations(ctx, at, batchSize)
	if err != nil {
		return err
	}
	for _, p := range lapsed {
		failed := &providers.ChargeResult{State: providers.StateFailed, FailureReason: payment.ReasonConfirmationExpired}
		if _, err := c.applyOutcome(ctx, p.ID, failed, at); err != nil {
			c.logFailure(err, "fail lapsed confirmation", logger.NewFields().Add("payment", p.ID.String()))
			errs = append(errs, err)
		}
	}

	issued, err := c.Invoices.FindIssuedBefore(ctx, at.Add(-uncollectedAfter), batchSize)
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	for _, inv := range issued {
		if err := c.collect(ctx, inv, at); err != nil {
			c.logFailure(err, "sweep issued invoice", logger.NewFields().Add("invoice", inv.ID.String()))
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func (c *Collection) collect(ctx context.Context, inv *invoice.Invoice, at time.Time) error {
	if inv.NotifiedAt == nil {
		if err := c.notifyIssued(ctx, inv, at); err != nil {
			return err
		}
		if err := c.Invoices.SetNotified(ctx, inv.ID, at); err != nil {
			return err
		}
	}
	_, err := c.charge(ctx, inv.ID, nil, payment.TriggerIssue, at)
	if errors.Is(err, invoice.ErrorPaymentInProgress) || errors.Is(err, invoice.ErrorNotPayable) {
		return nil
	}

	return err
}

// WarnExpiringCards emails the Team about a default card that stops working
// within cardExpiryNotice, once per card. A card saved without an expiry
// (Craftgate does not report it) is asked about at its provider: an expiry
// it reports is stored, its own "expires next month" verdict is taken as
// the warning. The warning is published before it is recorded: a crash in
// between sends it twice rather than never.
func (c *Collection) WarnExpiringCards(ctx context.Context, at time.Time) error {
	cards, err := c.PaymentMethods.FindUnwarnedDefaults(ctx)
	if err != nil {
		return err
	}

	var errs []error
	for _, m := range cards {
		expiring, err := c.expiring(ctx, m, at)
		if err == nil && expiring {
			err = c.warnExpiring(ctx, m, at)
		}
		if err != nil {
			c.logFailure(err, "warn expiring card", logger.NewFields().Add("payment_method", m.ID.String()))
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func (c *Collection) expiring(ctx context.Context, m *paymentmethod.PaymentMethod, at time.Time) (bool, error) {
	if _, known := m.ExpiresAt(); known {
		return m.ExpiresWithin(at, cardExpiryNotice), nil
	}
	adapter, err := c.Registry.Get(m.Provider)
	if err != nil {
		return false, err
	}
	status, err := adapter.CardStatus(ctx, providers.CardRef{CustomerRef: m.ProviderCustomerRef, CardRef: m.ProviderCardRef})
	if err != nil {
		return false, err
	}
	if status.ExpMonth > 0 && status.ExpYear > 0 {
		if err := c.PaymentMethods.UpdateExpiry(ctx, m.ID, status.ExpMonth, status.ExpYear); err != nil {
			return false, err
		}
		m.ExpMonth, m.ExpYear = status.ExpMonth, status.ExpYear

		return m.ExpiresWithin(at, cardExpiryNotice), nil
	}
	if !status.ExpiringSoon {
		return false, nil
	}
	// The provider's verdict is "expires next month" without the month;
	// the email states that month rather than none.
	next := time.Date(at.Year(), at.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	m.ExpMonth, m.ExpYear = int(next.Month()), next.Year()

	return true, nil
}

func (c *Collection) warnExpiring(ctx context.Context, m *paymentmethod.PaymentMethod, at time.Time) error {
	recipients, teamName, err := c.recipients(ctx, m.TeamID)
	if err != nil {
		return err
	}
	if err := c.Notifier.CardExpiring(ctx, events.CardExpiringBody{
		Recipients: recipients, TeamName: teamName, Brand: m.Brand, Last4: m.Last4, ExpMonth: m.ExpMonth, ExpYear: m.ExpYear,
	}); err != nil {
		return err
	}
	_, err = c.PaymentMethods.MarkExpiryNotified(ctx, m.ID, at)

	return err
}
