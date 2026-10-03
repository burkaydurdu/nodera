// Package producers writes this context's events through the Mongo outbox
// (docs/adr/0001) and registers their types on the broker.
package producers

import (
	"context"

	"github.com/piggystats/outbox"
	"github.com/piggystats/rabbitmq"

	"github.com/piggystats/nodera/contexts/billing/async/constants"
	"github.com/piggystats/nodera/contexts/billing/async/events"
	"github.com/piggystats/nodera/pkg/apperror"
	contextConstants "github.com/piggystats/nodera/pkg/constants"
	"github.com/piggystats/nodera/pkg/realtime"
	"github.com/piggystats/nodera/utils/hash"
	"github.com/piggystats/nodera/utils/str"
)

//go:generate mockgen -destination=../../mock/producers/outbox_client.go -package=mockproducers github.com/piggystats/nodera/contexts/billing/async/producers OutboxClient
type OutboxClient interface {
	PublishWithOutbox(ctx context.Context, correlationID string, message *outbox.Message) (string, error)
}

// Producer registers every event type on the broker at start-up.
type Producer struct {
	client rabbitmq.IClient
}

func NewProducer(client rabbitmq.IClient) *Producer {
	return &Producer{client: client}
}

func (p *Producer) AddPublisher(ctx context.Context) {
	p.client.AddPublisher(
		ctx,
		constants.CommonExchange,
		rabbitmq.Topic,
		events.TeamBillingStatusChangedEvent{},
		events.TeamDropletsDeletionRequestedEvent{},
		events.InvoiceIssuedEvent{},
		events.PaymentSucceededEvent{},
		events.PaymentFailedEvent{},
		events.PaymentConfirmationRequiredEvent{},
		events.TeamSuspendedEvent{},
		events.DropletsDeletionWarningEvent{},
		events.CardExpiringEvent{},
		realtime.Message{},
	)
}

// Publish writes one event through the outbox. It must run after the
// transaction it follows has committed (docs/adr/0001). A request carries the
// gateway's correlation id; a worker job carries none and gets a fresh one so
// the event can still be traced.
func Publish(ctx context.Context, client OutboxClient, routingKey string, event any) error {
	payload, err := events.ToJSONString(event)
	if err != nil {
		return apperror.ErrorCommon.Copy().SetError(err)
	}

	correlationID, _ := ctx.Value(contextConstants.CorrelationID).(string)
	if str.IsEmpty(correlationID) {
		correlationID = hash.GenerateUUID()
	}

	_, err = client.PublishWithOutbox(ctx, correlationID, &outbox.Message{
		ExchangeName: constants.CommonExchange,
		RoutingKey:   routingKey,
		Payload:      payload,
	})
	if err != nil {
		return apperror.ErrorCommon.Copy().SetError(err)
	}

	return nil
}
