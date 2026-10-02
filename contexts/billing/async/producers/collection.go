package producers

import (
	"context"

	"github.com/piggystats/nodera/contexts/billing/async/constants"
	"github.com/piggystats/nodera/contexts/billing/async/events"
)

// Notifications publishes the emails collection sends: one event per
// occasion, every recipient inside it, each email in its recipient's Locale.
type Notifications struct{ client OutboxClient }

func NewNotifications(client OutboxClient) *Notifications {
	return &Notifications{client: client}
}

func (p *Notifications) InvoiceIssued(ctx context.Context, body events.InvoiceIssuedBody) error {
	return Publish(ctx, p.client, constants.InvoiceIssued, &events.InvoiceIssuedEvent{Payload: body})
}

func (p *Notifications) PaymentSucceeded(ctx context.Context, body events.PaymentSucceededBody) error {
	return Publish(ctx, p.client, constants.PaymentSucceeded, &events.PaymentSucceededEvent{Payload: body})
}

func (p *Notifications) PaymentFailed(ctx context.Context, body events.PaymentFailedBody) error {
	return Publish(ctx, p.client, constants.PaymentFailed, &events.PaymentFailedEvent{Payload: body})
}

func (p *Notifications) PaymentConfirmationRequired(ctx context.Context, body events.PaymentConfirmationRequiredBody) error {
	return Publish(ctx, p.client, constants.PaymentConfirmationRequired, &events.PaymentConfirmationRequiredEvent{Payload: body})
}

func (p *Notifications) TeamSuspended(ctx context.Context, body events.TeamSuspendedBody) error {
	return Publish(ctx, p.client, constants.TeamSuspended, &events.TeamSuspendedEvent{Payload: body})
}

func (p *Notifications) DropletsDeletionWarning(ctx context.Context, body events.DropletsDeletionWarningBody) error {
	return Publish(ctx, p.client, constants.DropletsDeletionWarning, &events.DropletsDeletionWarningEvent{Payload: body})
}

func (p *Notifications) CardExpiring(ctx context.Context, body events.CardExpiringBody) error {
	return Publish(ctx, p.client, constants.CardExpiring, &events.CardExpiringEvent{Payload: body})
}
