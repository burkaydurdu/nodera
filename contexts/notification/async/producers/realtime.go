// Package producers sends what the notification context tells the web app.
// The context has no outbox: a push goes straight to RabbitMQ after the
// Notification is recorded, and the delivery state per Channel makes a
// redelivered event retry only what did not go out (docs/adr/0007).
package producers

import (
	"context"

	"github.com/piggystats/rabbitmq"

	"github.com/piggystats/nodera/contexts/notification/async/constants"
	"github.com/piggystats/nodera/pkg/realtime"
)

// Realtime publishes realtime messages on the common exchange.
type Realtime struct {
	client rabbitmq.IClient
}

func NewRealtime(client rabbitmq.IClient) *Realtime {
	return &Realtime{client: client}
}

// AddPublisher registers the message type on the broker at start-up.
func (p *Realtime) AddPublisher(ctx context.Context) {
	p.client.AddPublisher(ctx, constants.CommonExchange, rabbitmq.Topic, realtime.Message{})
}

func (p *Realtime) Publish(ctx context.Context, message *realtime.Message) error {
	return p.client.Publish(ctx, realtime.RoutingKey, constants.CommonExchange, message)
}
