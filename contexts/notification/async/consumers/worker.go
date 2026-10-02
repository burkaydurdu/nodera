package consumers

import (
	"context"

	"github.com/piggystats/rabbitmq"

	"github.com/piggystats/nodera/contexts/notification/async/constants"
	"github.com/piggystats/nodera/logger"
)

type Worker struct {
	client rabbitmq.IClient
	log    logger.ILog
	items  rabbitmq.ConsumerItems
}

func NewWorker(client rabbitmq.IClient, log logger.ILog, sender ISender) *Worker {
	bind := func(consumer rabbitmq.IConsumer, routingKey string) *rabbitmq.ConsumerItem {
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
			bind(NewEmailVerificationCode(sender), constants.EmailVerificationCode),
			bind(NewEmailAlreadyRegistered(sender), constants.EmailAlreadyRegistered),
			bind(NewMFAEmailCode(sender), constants.MFAEmailCode),
			bind(NewPasswordReset(sender), constants.PasswordReset),
			bind(NewTeamInvitation(sender), constants.TeamInvitation),
			bind(NewInvoiceIssued(sender, log), constants.InvoiceIssued),
			bind(NewPaymentSucceeded(sender, log), constants.PaymentSucceeded),
			bind(NewPaymentFailed(sender, log), constants.PaymentFailed),
			bind(NewPaymentConfirmationRequired(sender, log), constants.PaymentConfirmationRequired),
			bind(NewTeamSuspended(sender, log), constants.TeamSuspended),
			bind(NewDropletsDeletionWarning(sender, log), constants.DropletsDeletionWarning),
			bind(NewCardExpiring(sender, log), constants.CardExpiring),
		},
	}
}

func (w *Worker) Consume(_ context.Context) {
	for _, item := range w.items {
		w.client.
			AddConsumer(item.Config.RoutingKey).
			SubscriberExchange(item.Config.RoutingKey, item.Config.Topic, item.Config.Exchange).
			HandleConsumer(item.Consumer.Consume)
	}

	w.log.Info("notification worker has started")
}
