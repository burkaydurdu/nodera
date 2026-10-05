package consumers

import (
	"context"

	"github.com/piggystats/rabbitmq"

	"github.com/piggystats/nodera/contexts/billing/async/constants"
	"github.com/piggystats/nodera/logger"
)

// Source is a consumer together with the routing keys it handles; the usage
// and Mail Package consumers join the customer one as further Sources.
type Source interface {
	rabbitmq.IConsumer
	RoutingKeys() []string
}

type Worker struct {
	client rabbitmq.IClient
	log    logger.ILog
	items  rabbitmq.ConsumerItems
}

// NewWorker binds one queue per routing key on the common exchange, each
// prefixed so the events fan out to this context regardless of who else
// listens.
func NewWorker(client rabbitmq.IClient, log logger.ILog, sources ...Source) *Worker {
	items := rabbitmq.ConsumerItems{}
	for _, source := range sources {
		for _, key := range source.RoutingKeys() {
			items = append(items, &rabbitmq.ConsumerItem{
				Consumer: source,
				Config: rabbitmq.ConsumerConfig{
					RoutingKey: key,
					Topic:      rabbitmq.Topic,
					Exchange:   constants.CommonExchange,
				},
			})
		}
	}

	return &Worker{client: client, log: log, items: items}
}

func (w *Worker) Consume(_ context.Context) {
	for _, item := range w.items {
		w.client.
			AddConsumer(constants.QueuePrefix+item.Config.RoutingKey).
			SubscriberExchange(item.Config.RoutingKey, item.Config.Topic, item.Config.Exchange).
			HandleConsumer(item.Consumer.Consume)
	}

	w.log.Info("billing consumers have started")
}
