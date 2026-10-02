package handlers

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/piggystats/rerror"

	"github.com/piggystats/nodera/contexts/billing/aggregates/invoice"
	"github.com/piggystats/nodera/contexts/billing/aggregates/payment"
	"github.com/piggystats/nodera/contexts/billing/application/dto"
	"github.com/piggystats/nodera/contexts/billing/signing"
	"github.com/piggystats/nodera/pkg/authz"
	"github.com/piggystats/nodera/pkg/ratelimit"
	"github.com/piggystats/nodera/utils/str"
)

const (
	scopePayNow     = "billing:pay"
	payNowWindow    = time.Hour
	payNowPerWindow = 5
)

type Invoice struct {
	authorizer *Authorizer
	collection *Collection
	limiter    ratelimit.ILimiter
	now        func() time.Time
}

func NewInvoice(authorizer *Authorizer, collection *Collection, limiter ratelimit.ILimiter, now func() time.Time) *Invoice {
	return &Invoice{authorizer: authorizer, collection: collection, limiter: limiter, now: now}
}

func (h *Invoice) List(ctx context.Context, req *invoice.ListRequest) ([]dto.InvoiceSummary, error) {
	teamID := uuid.MustParse(req.TeamID)
	if _, err := h.authorizer.Authorize(ctx, teamID, authz.BillingRead); err != nil {
		return nil, err
	}

	invoices, err := h.collection.Invoices.FindByTeamID(ctx, teamID)
	if err != nil {
		return nil, err
	}

	return dto.ToInvoiceSummaries(invoices), nil
}

func (h *Invoice) Get(ctx context.Context, req *invoice.GetRequest) (*dto.Invoice, error) {
	teamID := uuid.MustParse(req.TeamID)
	if _, err := h.authorizer.Authorize(ctx, teamID, authz.BillingRead); err != nil {
		return nil, err
	}

	inv, err := h.collection.Invoices.FindByID(ctx, teamID, uuid.MustParse(req.InvoiceID))
	if err != nil {
		return nil, err
	}
	payments, err := h.collection.Payments.FindByInvoiceID(ctx, inv.ID)
	if err != nil {
		return nil, err
	}

	return dto.ToInvoice(inv, payments), nil
}

// PDF answers the Invoice's PDF and the number to name the file after.
func (h *Invoice) PDF(ctx context.Context, req *invoice.GetRequest) ([]byte, string, error) {
	teamID := uuid.MustParse(req.TeamID)
	if _, err := h.authorizer.Authorize(ctx, teamID, authz.BillingRead); err != nil {
		return nil, "", err
	}

	inv, err := h.collection.Invoices.FindByID(ctx, teamID, uuid.MustParse(req.InvoiceID))
	if err != nil {
		return nil, "", err
	}
	data, err := h.collection.PDF(ctx, inv)

	return data, inv.Number, err
}

// Download serves the signed link of the issue email: the token names the
// Invoice and its expiry, so it works signed out and for nothing else. An
// unknown Invoice answers like a bad token.
func (h *Invoice) Download(ctx context.Context, req *invoice.DownloadRequest) ([]byte, string, error) {
	id := uuid.MustParse(req.InvoiceID)
	if !h.collection.Signer.Verify(signing.PurposeInvoicePDF, id, time.Unix(req.Expires, 0), req.Token, h.now()) {
		return nil, "", invoice.ErrorLinkInvalid
	}

	inv, err := h.collection.Invoices.FindAnyByID(ctx, id)
	if errors.Is(err, invoice.ErrorNotFound) {
		return nil, "", invoice.ErrorLinkInvalid
	}
	if err != nil {
		return nil, "", err
	}
	data, err := h.collection.PDF(ctx, inv)

	return data, inv.Number, err
}

// Pay makes a Payment attempt now, with the default card or the one asked
// for. A paid or void Invoice is refused before the attempt counts against
// the Team's hourly budget.
func (h *Invoice) Pay(ctx context.Context, req *invoice.PayRequest) (*dto.Payment, error) {
	teamID := uuid.MustParse(req.TeamID)
	if _, err := h.authorizer.Authorize(ctx, teamID, authz.BillingWrite); err != nil {
		return nil, err
	}

	inv, err := h.collection.Invoices.FindByID(ctx, teamID, uuid.MustParse(req.InvoiceID))
	if err != nil {
		return nil, err
	}
	if !inv.Payable() {
		return nil, invoice.ErrorNotPayable
	}
	var methodID *uuid.UUID
	if str.IsNotEmpty(req.PaymentMethodID) {
		id := uuid.MustParse(req.PaymentMethodID)
		if _, err := h.collection.PaymentMethods.FindByID(ctx, teamID, id); err != nil {
			return nil, err
		}
		methodID = &id
	}
	// Checked again under the lock; checking first keeps a refused attempt
	// from spending the Team's hourly budget.
	if err := h.collection.refuseWhileLive(ctx, inv.ID, h.now().UTC()); err != nil {
		return nil, err
	}
	if err := h.limiter.Allow(ctx, scopePayNow, teamID.String(), ratelimit.Rule{Window: payNowWindow, Limit: payNowPerWindow}); err != nil {
		var limited rerror.Error
		if errors.As(err, &limited) {
			return nil, payment.ErrorPayRateLimited.Copy().AddFields(limited.GetFields())
		}

		return nil, err
	}

	p, err := h.collection.charge(ctx, inv.ID, methodID, payment.TriggerPayNow, h.now().UTC())
	if err != nil {
		return nil, err
	}

	return dto.ToPayment(p), nil
}
