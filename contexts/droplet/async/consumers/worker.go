package consumers

import (
	"context"

	"github.com/piggystats/rabbitmq"

	"github.com/piggystats/nodera/contexts/droplet/async/constants"
	"github.com/piggystats/nodera/logger"
)

type Worker struct {
	client rabbitmq.IClient
	log    logger.ILog
	items  rabbitmq.ConsumerItems
}

// NewWorker binds one consumer per routing key on the common exchange, each
// on its own queue so the events fan out to this context regardless of who
// else listens.
func NewWorker(client rabbitmq.IClient, log logger.ILog, consumer rabbitmq.IConsumer) *Worker {
	bind := func(routingKey string) *rabbitmq.ConsumerItem {
		return &rabbitmq.ConsumerItem{
			Consumer: consumer,
			Config: rabbitmq.ConsumerConfig{
				RoutingKey: routingKey,
				Topic:      rabbitmq.Topic,
				Exchange:   constants.CommonExchange,
			},
		}
	}

	return &Worker{
		client: client,
		log:    log,
		items: rabbitmq.ConsumerItems{
			bind(constants.TeamCreated),
			bind(constants.TeamMemberAdded),
			bind(constants.TeamMemberRoleChanged),
			bind(constants.TeamMemberRemoved),
			bind(constants.ProjectCreated),
		},
	}
}

func (w *Worker) Consume(_ context.Context) {
	for _, item := range w.items {
		w.client.
			AddConsumer(constants.QueuePrefix+item.Config.RoutingKey).
			SubscriberExchange(item.Config.RoutingKey, item.Config.Topic, item.Config.Exchange).
			HandleConsumer(item.Consumer.Consume)
	}

	w.log.Info("droplet customer-events consumer has started")
}
