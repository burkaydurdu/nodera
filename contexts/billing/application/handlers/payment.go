package handlers

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/billing/aggregates/payment"
	"github.com/piggystats/nodera/contexts/billing/application/dto"
	"github.com/piggystats/nodera/utils/str"
)

// Payment serves the public confirmation routes: the token of the emailed
// link authorizes one attempt, so none of them needs a session.
type Payment struct {
	collection *Collection
	now        func() time.Time
}

func NewPayment(collection *Collection, now func() time.Time) *Payment {
	return &Payment{collection: collection, now: now}
}

// Confirm answers the attempt as it stands, for the page the link opens.
func (h *Payment) Confirm(ctx context.Context, req *payment.ConfirmRequest) (*dto.Payment, error) {
	p, err := h.collection.ConfirmationStatus(ctx, uuid.MustParse(req.PaymentID), req.Token, h.now().UTC())
	if err != nil {
		return nil, err
	}

	return dto.ToPayment(p), nil
}

// StartConfirmation opens the bank's 3-D Secure step for the cardholder who
// followed the link: a page to render, or one to redirect to.
func (h *Payment) StartConfirmation(ctx context.Context, req *payment.StartConfirmationRequest) (*dto.ConfirmationStart, error) {
	confirmation, err := h.collection.StartConfirmation(ctx, uuid.MustParse(req.PaymentID), req.Token, h.now().UTC())
	if err != nil {
		return nil, err
	}

	out := &dto.ConfirmationStart{}
	if str.IsNotEmpty(confirmation.HTML) {
		out.HTMLContent = &confirmation.HTML
	}
	if str.IsNotEmpty(confirmation.RedirectURL) {
		out.RedirectURL = &confirmation.RedirectURL
	}

	return out, nil
}

// Callback is where the provider sends the cardholder back after 3-D
// Secure; it answers the web app page that shows the outcome.
func (h *Payment) Callback(ctx context.Context, req *payment.CallbackRequest) (string, error) {
	return h.collection.CompleteConfirmation(ctx, uuid.MustParse(req.PaymentID), req.Token, req.Callback, h.now().UTC())
}
