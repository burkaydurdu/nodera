package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/billing/aggregates/billingstatus"
	"github.com/piggystats/nodera/contexts/billing/aggregates/invoice"
	"github.com/piggystats/nodera/contexts/billing/aggregates/payment"
	"github.com/piggystats/nodera/contexts/billing/aggregates/paymentmethod"
	"github.com/piggystats/nodera/contexts/billing/async/events"
	"github.com/piggystats/nodera/contexts/billing/providers"
	"github.com/piggystats/nodera/contexts/billing/signing"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/utils/str"
)

const (
	callbackPath  = "/v1/billing/payments/%s/3ds/callback?token=%s"
	resultPath    = "/billing/confirm?payment=%s&result=%s"
	resultSuccess = "succeeded"
	resultFailure = "failed"
)

// charge makes one Payment attempt on the Invoice: with the given card, or
// the Team's default. The attempt is recorded before the provider is called;
// one the provider never answered stays pending for reconciliation, which
// looks it up before charging again. No attempt starts while another may
// still collect, so two attempts never race to charge the same Invoice.
func (c *Collection) charge(ctx context.Context, invoiceID uuid.UUID, methodID *uuid.UUID, trigger payment.Trigger, at time.Time) (*payment.Payment, error) {
	current, err := c.Invoices.FindAnyByID(ctx, invoiceID)
	if err != nil {
		return nil, err
	}
	teamID := current.TeamID

	var (
		p      *payment.Payment
		inv    *invoice.Invoice
		method *paymentmethod.PaymentMethod
		reason string
	)
	err = inTransaction(ctx, c.Transactions, func(ctx context.Context) error {
		if _, err := c.Statuses.FindForUpdate(ctx, teamID, at); err != nil {
			return err
		}
		inv, err = c.Invoices.FindForUpdate(ctx, invoiceID)
		if err != nil {
			return err
		}
		if !inv.Payable() {
			return invoice.ErrorNotPayable
		}
		if err := c.refuseWhileLive(ctx, invoiceID, at); err != nil {
			return err
		}
		method, err = c.cardFor(ctx, teamID, methodID)
		if err != nil {
			return err
		}
		attempt, err := c.Payments.NextAttempt(ctx, invoiceID)
		if err != nil {
			return err
		}

		expires := at.Add(confirmLinkLifetime).Truncate(time.Second)
		p = &payment.Payment{
			ID: uuid.New(), InvoiceID: invoiceID, TeamID: teamID, Attempt: attempt, Trigger: trigger,
			Status: payment.StatusPending, Amount: inv.TotalDue, Currency: inv.Currency, ConfirmExpiresAt: &expires, CreatedAt: at,
		}
		if method == nil {
			reason = payment.ReasonNoPaymentMethod
		} else {
			p.PaymentMethodID, p.Provider = &method.ID, method.Provider
			amount, ok := inv.AmountIn(method.Currency)
			if ok {
				p.Amount, p.Currency = amount, method.Currency
			} else {
				reason = payment.ReasonCurrencyMismatch
			}
		}

		return c.Payments.Create(ctx, p)
	})
	if err != nil {
		return nil, err
	}

	if str.IsNotEmpty(reason) {
		return c.applyOutcome(ctx, p.ID, &providers.ChargeResult{State: providers.StateFailed, FailureReason: reason}, at)
	}

	result, err := c.callCharge(ctx, p, method, inv.Number)
	if err != nil {
		c.logFailure(err, "charge", logger.NewFields().Add("payment", p.ID.String()))

		return p, nil
	}

	return c.applyOutcome(ctx, p.ID, result, at)
}

// refuseWhileLive answers ErrorPaymentInProgress while an attempt of the
// Invoice is pending or its confirmation link is still open.
func (c *Collection) refuseWhileLive(ctx context.Context, invoiceID uuid.UUID, at time.Time) error {
	attempts, err := c.Payments.FindByInvoiceID(ctx, invoiceID)
	if err != nil {
		return err
	}
	for _, a := range attempts {
		if a.Live(at) {
			return invoice.ErrorPaymentInProgress
		}
	}

	return nil
}

// cardFor answers the card to charge: the one asked for, which must be a
// live card of the Team, or the default; nil when the Team has none.
func (c *Collection) cardFor(ctx context.Context, teamID uuid.UUID, methodID *uuid.UUID) (*paymentmethod.PaymentMethod, error) {
	if methodID != nil {
		return c.PaymentMethods.FindByID(ctx, teamID, *methodID)
	}
	methods, err := c.PaymentMethods.FindByTeamID(ctx, teamID)
	if err != nil {
		return nil, err
	}

	return methods.Default(), nil
}

// callCharge asks the provider; a decline is an outcome, anything else is the
// provider being unreachable and leaves the attempt pending.
func (c *Collection) callCharge(ctx context.Context, p *payment.Payment, method *paymentmethod.PaymentMethod, number string) (*providers.ChargeResult, error) {
	adapter, err := c.Registry.Get(method.Provider)
	if err != nil {
		return nil, err
	}
	result, err := adapter.Charge(ctx, chargeRequest(p, method, number))
	if reason, declined := providers.Declined(err); declined {
		return &providers.ChargeResult{State: providers.StateFailed, FailureReason: reason}, nil
	}

	return result, err
}

func chargeRequest(p *payment.Payment, method *paymentmethod.PaymentMethod, number string) providers.ChargeRequest {
	return providers.ChargeRequest{
		Card:               providers.CardRef{CustomerRef: method.ProviderCustomerRef, CardRef: method.ProviderCardRef},
		Amount:             p.Amount,
		Currency:           p.Currency,
		IdempotencyKey:     p.IdempotencyKey,
		Description:        "Nodera " + number,
		ProviderPaymentRef: p.ProviderRef,
	}
}

// outcome is what applying a provider answer changed, for the events
// published after the commit.
type outcome struct {
	payment   *payment.Payment
	invoice   *invoice.Invoice
	succeeded bool
	failed    bool
	confirm   bool
	status    *statusChange
}

// applyOutcome records the provider's answer on the Payment and its Invoice
// once: a Payment already settled ignores later answers, so a replayed
// webhook, a reconciliation and a callback racing each other publish one set
// of events.
func (c *Collection) applyOutcome(ctx context.Context, paymentID uuid.UUID, result *providers.ChargeResult, at time.Time) (*payment.Payment, error) {
	current, err := c.Payments.FindByID(ctx, paymentID)
	if err != nil {
		return nil, err
	}

	var out outcome
	err = inTransaction(ctx, c.Transactions, func(ctx context.Context) error {
		if _, err := c.Statuses.FindForUpdate(ctx, current.TeamID, at); err != nil {
			return err
		}
		p, err := c.Payments.FindForUpdate(ctx, paymentID)
		if err != nil {
			return err
		}
		out.payment = p
		if p.Settled() {
			return nil
		}
		if str.IsNotEmpty(result.ProviderPaymentRef) {
			p.ProviderRef = result.ProviderPaymentRef
		}

		switch result.State {
		case providers.StateSucceeded:
			p.Status, p.CompletedAt = payment.StatusSucceeded, &at
			out.invoice, err = c.markPaid(ctx, p, at)
			out.succeeded = out.invoice != nil
		case providers.StateFailed:
			p.Status, p.FailureReason, p.CompletedAt = payment.StatusFailed, result.FailureReason, &at
			if str.IsEmpty(p.FailureReason) {
				p.FailureReason = providers.ReasonDeclined
			}
			out.invoice, err = c.markFailed(ctx, p, at)
			out.failed = out.invoice != nil
		case providers.StateConfirmationRequired:
			out.confirm = p.Status != payment.StatusConfirmationRequired
			p.Status = payment.StatusConfirmationRequired
			// The link the cardholder follows is the web app's page; the
			// provider's 3-D Secure step opens only from there.
			p.ConfirmationURL = c.confirmURL(p)
			if out.confirm {
				out.invoice, err = c.Invoices.FindForUpdate(ctx, p.InvoiceID)
			}
		default:
			// Still pending at the provider: only the reference is new.
		}
		if err != nil {
			return err
		}
		if err := c.Payments.Save(ctx, p); err != nil {
			return err
		}
		out.status, err = c.settleStatus(ctx, p.TeamID, at)

		return err
	})
	if err != nil {
		return nil, err
	}

	c.publishOutcome(ctx, &out)

	return out.payment, nil
}

// markPaid pays the Invoice unless it no longer owes anything; a second
// successful attempt on a paid Invoice is logged for a refund by hand.
func (c *Collection) markPaid(ctx context.Context, p *payment.Payment, at time.Time) (*invoice.Invoice, error) {
	inv, err := c.Invoices.FindForUpdate(ctx, p.InvoiceID)
	if err != nil {
		return nil, err
	}
	if !inv.Payable() {
		c.Log.WarnWithFields("payment succeeded on an invoice that no longer owes", logger.NewFields().
			Add("invoice", inv.ID.String()).Add("payment", p.ID.String()))

		return nil, nil
	}
	inv.Status, inv.PaidAt = invoice.StatusPaid, &at

	return inv, c.Invoices.Save(ctx, inv)
}

// markFailed starts Dunning on the first failure; later failures leave the
// schedule where it is.
func (c *Collection) markFailed(ctx context.Context, p *payment.Payment, at time.Time) (*invoice.Invoice, error) {
	inv, err := c.Invoices.FindForUpdate(ctx, p.InvoiceID)
	if err != nil {
		return nil, err
	}
	if !inv.Payable() {
		return nil, nil
	}
	if inv.Status == invoice.StatusIssued {
		inv.Status, inv.FailedAt, inv.DunningStep = invoice.StatusPaymentFailed, &at, invoice.StepNone
		if err := c.Invoices.Save(ctx, inv); err != nil {
			return nil, err
		}
	}

	return inv, nil
}

// statusChange is a Billing status move to announce: the row as moved, and
// the Invoice that caused a suspension.
type statusChange struct {
	status     *billingstatus.TeamStatus
	suspending *invoice.Invoice
}

// settleStatus derives the Team's Billing status and its Dunning fields from
// its payment_failed Invoices: the oldest is the open one, the earliest
// scheduled retry is the next, and any Invoice that reached its day-7 step
// keeps the Team suspended until it is paid. Runs inside the caller's
// transaction with the status row locked.
func (c *Collection) settleStatus(ctx context.Context, teamID uuid.UUID, at time.Time) (*statusChange, error) {
	st, err := c.Statuses.FindForUpdate(ctx, teamID, at)
	if err != nil {
		return nil, err
	}
	failed, err := c.Invoices.FindFailedForTeam(ctx, teamID)
	if err != nil {
		return nil, err
	}

	previous := st.Status
	st.OpenInvoiceID, st.NextRetryAt = nil, nil
	var suspending *invoice.Invoice
	for _, inv := range failed {
		if inv.FailedAt == nil {
			continue
		}
		if st.OpenInvoiceID == nil {
			id := inv.ID
			st.OpenInvoiceID = &id
		}
		if next := invoice.NextRetryAt(*inv.FailedAt, inv.DunningStep); next != nil && (st.NextRetryAt == nil || next.Before(*st.NextRetryAt)) {
			st.NextRetryAt = next
		}
		if suspending == nil && inv.DunningStep >= invoice.StepSuspend {
			suspending = inv
		}
	}

	switch {
	case suspending != nil:
		deletionAt := invoice.DeletionAt(*suspending.FailedAt)
		st.DeletionAt = &deletionAt
		if st.Status != billingstatus.Suspended {
			st.Move(billingstatus.Suspended, at)
			st.SuspendedAt = &st.ChangedAt
		}
	case st.Status == billingstatus.Suspended:
		count, err := c.PaymentMethods.CountByTeamID(ctx, teamID)
		if err != nil {
			return nil, err
		}
		to := billingstatus.Active
		if count == 0 {
			to = billingstatus.NoPaymentMethod
		}
		st.Move(to, at)
		st.SuspendedAt, st.DeletionAt = nil, nil
	}
	if err := c.Statuses.Save(ctx, st); err != nil {
		return nil, err
	}
	if st.Status == previous {
		return nil, nil
	}

	return &statusChange{status: st, suspending: suspending}, nil
}

// settle re-derives the Team's status in its own transaction and announces
// a change; the Dunning schedule runs it so a step suspends the Team whatever
// the attempt on that day answered.
func (c *Collection) settle(ctx context.Context, teamID uuid.UUID, at time.Time) error {
	var change *statusChange
	err := inTransaction(ctx, c.Transactions, func(ctx context.Context) error {
		var err error
		change, err = c.settleStatus(ctx, teamID, at)

		return err
	})
	if err != nil {
		return err
	}
	c.announceStatus(ctx, teamID, change)

	return nil
}

// publishOutcome sends what an applied answer announces. The status event
// goes first and on its own: the droplet context must learn a suspension or
// recovery even when the emails cannot be addressed. Failures are logged;
// the rows already hold the truth.
func (c *Collection) publishOutcome(ctx context.Context, out *outcome) {
	p := out.payment
	if p == nil {
		return
	}
	c.announceStatus(ctx, p.TeamID, out.status)
	if !out.succeeded && !out.failed && !out.confirm {
		return
	}
	fields := func() logger.Fields { return logger.NewFields().Add("payment", p.ID.String()) }

	address, err := c.invoiceAddress(ctx, p.TeamID)
	if err != nil {
		c.logFailure(err, "address payment emails", fields())

		return
	}
	number := ""
	if out.invoice != nil {
		number = out.invoice.Number
	}

	switch {
	case out.succeeded:
		err = c.Notifier.PaymentSucceeded(ctx, events.PaymentSucceededBody{
			TeamID: p.TeamID, InvoiceID: p.InvoiceID, PaymentID: p.ID, InvoiceAddress: address, InvoiceNumber: number,
			Amount: p.Amount.StringFixed(2), Currency: p.Currency, PaidAt: p.CompletedAt.UTC(),
		})
	case out.failed:
		var next *time.Time
		if out.invoice.FailedAt != nil {
			next = utcPtr(invoice.NextRetryAt(*out.invoice.FailedAt, out.invoice.DunningStep))
		}
		err = c.Notifier.PaymentFailed(ctx, events.PaymentFailedBody{
			TeamID: p.TeamID, InvoiceID: p.InvoiceID, PaymentID: p.ID, InvoiceAddress: address, InvoiceNumber: number,
			Amount: p.Amount.StringFixed(2), Currency: p.Currency, FailureReason: p.FailureReason, Attempt: p.Attempt, NextRetryAt: next,
		})
	case out.confirm:
		err = c.Notifier.PaymentConfirmationRequired(ctx, events.PaymentConfirmationRequiredBody{
			TeamID: p.TeamID, InvoiceID: p.InvoiceID, PaymentID: p.ID, InvoiceAddress: address, InvoiceNumber: number,
			Amount: p.Amount.StringFixed(2), Currency: p.Currency, ConfirmURL: c.confirmURL(p),
		})
	}
	if err != nil {
		c.logFailure(err, "publish payment outcome", fields())
	}
}

// announceStatus tells the droplet context about a status move with the
// row's own change time and, on a suspension, announces it for the Team's
// notifications. The status event never waits on the invoice address.
func (c *Collection) announceStatus(ctx context.Context, teamID uuid.UUID, change *statusChange) {
	if change == nil {
		return
	}
	st := change.status
	fields := func() logger.Fields {
		return logger.NewFields().Add("team", teamID.String()).Add("status", string(st.Status))
	}
	if err := c.StatusChanged.Publish(ctx, teamID, string(st.Status), st.ChangedAt); err != nil {
		c.logFailure(err, "publish billing status", fields())
	}
	if st.Status != billingstatus.Suspended || change.suspending == nil {
		return
	}

	address, err := c.invoiceAddress(ctx, teamID)
	if err == nil {
		err = c.Notifier.TeamSuspended(ctx, events.TeamSuspendedBody{
			TeamID: teamID, InvoiceID: change.suspending.ID, InvoiceAddress: address, InvoiceNumber: change.suspending.Number,
			PayURL: c.payURL(teamID, change.suspending.ID),
		})
	}
	if err != nil {
		c.logFailure(err, "publish team suspended", fields())
	}
}

// ConfirmationStatus answers the attempt behind a confirmation link as it
// stands; it never calls the provider, the 3-D Secure step and its callback
// do.
func (c *Collection) ConfirmationStatus(ctx context.Context, paymentID uuid.UUID, token string, at time.Time) (*payment.Payment, error) {
	return c.authorizedAttempt(ctx, paymentID, token, at)
}

// StartConfirmation opens the provider's 3-D Secure step for an attempt the
// bank refused without the cardholder, now that the cardholder followed the
// link. The provider calls back to the service, which completes the charge.
func (c *Collection) StartConfirmation(ctx context.Context, paymentID uuid.UUID, token string, at time.Time) (*providers.Confirmation, error) {
	p, err := c.authorizedAttempt(ctx, paymentID, token, at)
	if err != nil {
		return nil, err
	}
	if p.Status != payment.StatusConfirmationRequired || p.PaymentMethodID == nil {
		return nil, payment.ErrorNotAwaitingConfirmation
	}
	method, err := c.PaymentMethods.FindAnyByID(ctx, p.TeamID, *p.PaymentMethodID)
	if err != nil {
		return nil, err
	}
	inv, err := c.Invoices.FindAnyByID(ctx, p.InvoiceID)
	if err != nil {
		return nil, err
	}
	adapter, err := c.Registry.Get(p.Provider)
	if err != nil {
		c.logFailure(err, "start confirmation", logger.NewFields().Add("payment", p.ID.String()))

		return nil, payment.ErrorProviderUnavailable
	}

	req := chargeRequest(p, method, inv.Number)
	req.ReturnURL = c.callbackURL(p)
	confirmation, err := adapter.StartConfirmation(ctx, req)
	if reason, declined := providers.Declined(err); declined {
		if _, err := c.applyOutcome(ctx, p.ID, &providers.ChargeResult{State: providers.StateFailed, FailureReason: reason}, at); err != nil {
			return nil, err
		}

		return nil, payment.ErrorConfirmationDeclined
	}
	if err != nil {
		c.logFailure(err, "start confirmation", logger.NewFields().Add("payment", p.ID.String()))

		return nil, payment.ErrorProviderUnavailable
	}
	if str.IsBlank(confirmation.HTML) && str.IsBlank(confirmation.RedirectURL) {
		c.logFailure(errors.New("confirmation without a page or a redirect"), "start confirmation", logger.NewFields().Add("payment", p.ID.String()))

		return nil, payment.ErrorProviderUnavailable
	}
	if err := c.recordConfirmation(ctx, p.ID, confirmation.ProviderPaymentRef); err != nil {
		return nil, err
	}

	return confirmation, nil
}

// recordConfirmation keeps the provider's reference of the 3-D Secure
// payment, which its callback must name.
func (c *Collection) recordConfirmation(ctx context.Context, paymentID uuid.UUID, ref string) error {
	if str.IsBlank(ref) {
		return nil
	}

	return inTransaction(ctx, c.Transactions, func(ctx context.Context) error {
		p, err := c.Payments.FindForUpdate(ctx, paymentID)
		if err != nil || p.Status != payment.StatusConfirmationRequired {
			return err
		}
		p.ProviderRef = ref

		return c.Payments.Save(ctx, p)
	})
}

// CompleteConfirmation is the provider's 3-D Secure callback: the link's
// token names the attempt, the provider's own check names the payment, and
// both must agree before the charge is completed. It answers where the
// browser goes next. A callback for an attempt already settled answers its
// result again without asking the provider.
func (c *Collection) CompleteConfirmation(ctx context.Context, paymentID uuid.UUID, token string, callback map[string]string, at time.Time) (string, error) {
	p, err := c.authorizedAttempt(ctx, paymentID, token, at)
	if err != nil {
		return "", err
	}
	if p.Status == payment.StatusConfirmationRequired {
		adapter, err := c.Registry.Get(p.Provider)
		if err != nil {
			c.logFailure(err, "complete confirmation", logger.NewFields().Add("payment", p.ID.String()))

			return "", payment.ErrorProviderUnavailable
		}
		result, err := adapter.CompleteConfirmation(ctx, callback)
		if errors.Is(err, providers.ErrBadSignature) || errors.Is(err, providers.ErrUnknownRef) {
			return "", payment.ErrorCallbackNotAuthentic
		}
		if err != nil {
			c.logFailure(err, "complete confirmation", logger.NewFields().Add("payment", p.ID.String()))

			return "", payment.ErrorProviderUnavailable
		}
		if result.ProviderPaymentRef != p.ProviderRef || (str.IsNotEmpty(result.IdempotencyKey) && result.IdempotencyKey != p.IdempotencyKey) {
			return "", payment.ErrorCallbackNotAuthentic
		}
		if p, err = c.applyOutcome(ctx, p.ID, result, at); err != nil {
			return "", err
		}
	}

	outcome := resultFailure
	if p.Status == payment.StatusSucceeded {
		outcome = resultSuccess
	}

	return c.FrontURL + fmt.Sprintf(resultPath, p.ID, outcome), nil
}

// authorizedAttempt answers the attempt a confirmation token was minted for;
// an unknown attempt and a forged or lapsed token are the same answer.
func (c *Collection) authorizedAttempt(ctx context.Context, paymentID uuid.UUID, token string, at time.Time) (*payment.Payment, error) {
	p, err := c.Payments.FindByID(ctx, paymentID)
	if errors.Is(err, payment.ErrorNotFound) {
		return nil, payment.ErrorConfirmTokenInvalid
	}
	if err != nil {
		return nil, err
	}
	if p.ConfirmExpiresAt == nil || !c.Signer.Verify(signing.PurposePaymentConfirm, p.ID, *p.ConfirmExpiresAt, token, at) {
		return nil, payment.ErrorConfirmTokenInvalid
	}

	return p, nil
}

func (c *Collection) callbackURL(p *payment.Payment) string {
	token := c.Signer.Sign(signing.PurposePaymentConfirm, p.ID, *p.ConfirmExpiresAt)

	return c.GatewayURL + fmt.Sprintf(callbackPath, p.ID, url.QueryEscape(token))
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()

	return &u
}
