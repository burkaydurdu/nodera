package consumers

import (
	"context"

	"github.com/piggystats/rabbitmq"

	"github.com/piggystats/nodera/contexts/notification/async/constants"
	"github.com/piggystats/nodera/logger"
)

// Source is a consumer with the routing keys it handles; each key gets its own
// queue on the common exchange.
type Source interface {
	rabbitmq.IConsumer
	RoutingKeys() []string
}

// bareQueues are the email events' queues, named before the prefix existed.
// They keep their names, so no queue is left bound with nobody reading it.
var bareQueues = map[string]bool{
	constants.EmailVerificationCode: true, constants.EmailAlreadyRegistered: true, constants.MFAEmailCode: true,
	constants.PasswordReset: true, constants.TeamInvitation: true,
	constants.InvoiceIssued: true, constants.PaymentSucceeded: true, constants.PaymentFailed: true,
	constants.PaymentConfirmationRequired: true, constants.TeamSuspended: true, constants.DropletsDeletionWarning: true,
	constants.CardExpiring: true,
}

type Worker struct {
	client rabbitmq.IClient
	log    logger.ILog
	items  rabbitmq.ConsumerItems
}

func NewWorker(client rabbitmq.IClient, log logger.ILog, sources ...Source) *Worker {
	items := rabbitmq.ConsumerItems{}
	for _, source := range sources {
		for _, key := range source.RoutingKeys() {
			items = append(items, &rabbitmq.ConsumerItem{
				Consumer: source,
				Config:   rabbitmq.ConsumerConfig{RoutingKey: key, Topic: rabbitmq.Topic, Exchange: constants.CommonExchange},
			})
		}
	}

	return &Worker{client: client, log: log, items: items}
}

func (w *Worker) Consume(_ context.Context) {
	for _, item := range w.items {
		queue := constants.QueuePrefix + item.Config.RoutingKey
		if bareQueues[item.Config.RoutingKey] {
			queue = item.Config.RoutingKey
		}
		w.client.
			AddConsumer(queue).
			SubscriberExchange(item.Config.RoutingKey, item.Config.Topic, item.Config.Exchange).
			HandleConsumer(item.Consumer.Consume)
	}

	w.log.Info("notification worker has started")
}
