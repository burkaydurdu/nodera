package producers

import (
	"context"

	"github.com/piggystats/outbox"
	"github.com/piggystats/rabbitmq"

	"github.com/piggystats/nodera/contexts/customer/async/constants"
	"github.com/piggystats/nodera/contexts/customer/async/events"
	"github.com/piggystats/nodera/pkg/apperror"
	contextConstants "github.com/piggystats/nodera/pkg/constants"
)

//go:generate mockgen -destination=../../mock/producers/outbox_client.go -package=mockproducers github.com/piggystats/nodera/contexts/customer/async/producers OutboxClient
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
		events.EmailVerificationCodeEvent{},
		events.EmailAlreadyRegisteredEvent{},
		events.MFAEmailCodeEvent{},
		events.PasswordResetEvent{},
		events.TeamInvitationEvent{},
		events.TeamCreatedEvent{},
		events.TeamMemberAddedEvent{},
		events.TeamMemberRoleChangedEvent{},
		events.TeamMemberRemovedEvent{},
		events.UserProfileUpdatedEvent{},
		events.ProjectCreatedEvent{},
	)
}

// publish writes one event through the outbox. It must run after the
// transaction it follows has committed (docs/adr/0001).
func publish(ctx context.Context, client OutboxClient, routingKey string, event any) error {
	payload, err := events.ToJSONString(event)
	if err != nil {
		return apperror.ErrorCommon.Copy().SetError(err)
	}

	correlationID, ok := ctx.Value(contextConstants.CorrelationID).(string)
	if !ok {
		return apperror.ErrorMissingCorrelationID
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
