package handlers

import (
	"context"
	"errors"
	"time"

	"github.com/piggystats/nodera/contexts/billing/aggregates/payment"
	"github.com/piggystats/nodera/contexts/billing/providers"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/ratelimit"
)

const (
	scopeWebhook       = "billing:webhook"
	webhookRateWindow  = time.Minute
	webhookRatePerHost = 300
)

type Webhook struct {
	log      logger.ILog
	payments payment.IService
	registry ProviderRegistry
	limiter  ratelimit.ILimiter
}

func NewWebhook(log logger.ILog, payments payment.IService, registry ProviderRegistry, limiter ratelimit.ILimiter) *Webhook {
	return &Webhook{log: log, payments: payments, registry: registry, limiter: limiter}
}

// Receive verifies the signature and stores the event once by the
// provider's event id. It never touches an Invoice: reconciliation picks the
// stored event up and asks the provider's API for the payment's state.
func (h *Webhook) Receive(ctx context.Context, req *payment.WebhookRequest) error {
	if err := h.limiter.Allow(ctx, scopeWebhook, req.ClientIP, ratelimit.Rule{Window: webhookRateWindow, Limit: webhookRatePerHost}); err != nil {
		return err
	}

	adapter, err := h.registry.Get(req.Provider)
	if err != nil {
		return payment.ErrorUnknownWebhook
	}

	event, err := adapter.VerifyWebhook(ctx, req.Headers, req.Body)
	if err != nil {
		if !errors.Is(err, providers.ErrBadSignature) {
			h.log.ErrorWithFields(err, logger.NewFields().Add("provider", req.Provider))
		}

		return payment.ErrorWebhookSignature
	}

	stored, err := h.payments.StoreWebhookEvent(ctx, &payment.WebhookEvent{
		Provider:           req.Provider,
		ProviderEventID:    event.ProviderEventID,
		Kind:               string(event.Kind),
		ProviderPaymentRef: event.ProviderPaymentRef,
		Status:             string(event.State),
		Body:               req.Body,
	})
	if err != nil {
		return err
	}
	if !stored {
		h.log.InfoWithFields("duplicate webhook ignored", logger.NewFields().Add("provider", req.Provider).Add("event", event.ProviderEventID))
	}

	return nil
}
