package handlers

import (
	"context"

	"github.com/piggystats/nodera/contexts/mail/aggregates/message"
	"github.com/piggystats/nodera/contexts/mail/aggregates/webhook"
)

// Retention deletes logs and Webhook deliveries past 30 days.
type Retention struct {
	messages message.IService
	webhooks webhook.IService
	now      Clock
}

func NewRetention(messages message.IService, webhooks webhook.IService, now Clock) *Retention {
	return &Retention{messages: messages, webhooks: webhooks, now: now}
}

func (h *Retention) Expire(ctx context.Context) error {
	before := h.now().Add(-message.Retention)
	if _, err := h.messages.ExpireCreatedBefore(ctx, before); err != nil {
		return err
	}

	return h.webhooks.ExpireBefore(ctx, before)
}
