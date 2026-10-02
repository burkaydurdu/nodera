// Package producers writes this context's events through the Mongo outbox
// (docs/adr/0001); the `outbox` command relays them to RabbitMQ.
package producers

import (
	"context"

	"github.com/google/uuid"
	"github.com/piggystats/outbox"
	"github.com/piggystats/rabbitmq"

	"github.com/piggystats/nodera/contexts/droplet/async/constants"
	"github.com/piggystats/nodera/contexts/droplet/async/events"
	"github.com/piggystats/nodera/pkg/apperror"
	contextConstants "github.com/piggystats/nodera/pkg/constants"
	"github.com/piggystats/nodera/utils/str"
)

//go:generate mockgen -destination=../../mock/producers/outbox_client.go -package=mockproducers github.com/piggystats/nodera/contexts/droplet/async/producers OutboxClient
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
		events.DropletBillingStartedEvent{},
		events.DropletBillingStoppedEvent{},
	)
}

// publish writes one event through the outbox. It must run after the
// transaction it follows has committed (docs/adr/0001). A request carries
// its correlation id; the poller and the consumers, which no request
// started, get one minted here so the event is still traceable downstream.
func publish(ctx context.Context, client OutboxClient, routingKey string, event any) error {
	payload, err := events.ToJSONString(event)
	if err != nil {
		return apperror.ErrorCommon.Copy().SetError(err)
	}

	correlationID, _ := ctx.Value(contextConstants.CorrelationID).(string)
	if str.IsEmpty(correlationID) {
		correlationID = uuid.NewString()
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
