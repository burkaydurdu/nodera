// Package consumers takes the contexts' realtime messages off RabbitMQ.
package consumers

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/piggystats/rabbitmq"

	"github.com/piggystats/nodera/pkg/realtime"
	"github.com/piggystats/nodera/utils/str"
)

const (
	CommonExchange = "CommonExchange"
	// QueuePrefix names this service's queue on the common exchange. Every
	// replica consumes the one queue; the fan-out reaches the others.
	QueuePrefix = "realtime."
)

var ErrMalformedMessage = errors.New("malformed realtime message")

type Publisher interface {
	Publish(ctx context.Context, body realtime.Body) error
}

// Messages hands every valid realtime message to the fan-out. A message no
// socket could ever accept is refused, so the producer's mistake shows in
// the error queue instead of vanishing.
type Messages struct {
	fanout Publisher
}

func NewMessages(fanout Publisher) *Messages {
	return &Messages{fanout: fanout}
}

func (c *Messages) Consume(message *rabbitmq.Message) error {
	var event realtime.Message
	if err := json.Unmarshal(message.Payload, &event); err != nil {
		return errors.Join(ErrMalformedMessage, err)
	}
	body := event.Payload
	if !body.Address.Valid() || str.IsBlank(body.Type) || !json.Valid(body.Data) {
		return ErrMalformedMessage
	}

	return c.fanout.Publish(context.Background(), body)
}
