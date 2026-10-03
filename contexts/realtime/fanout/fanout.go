// Package fanout carries a message from the replica that consumed it to every
// replica. A RabbitMQ queue hands each message to one consumer, while the
// sockets it is for may be held by any replica, so the consumer republishes
// it on one Redis pub/sub channel every replica subscribes to
// (docs/adr/0007). Pub/sub keeps nothing: a replica that is down misses the
// message, and its sockets were closed anyway.
package fanout

import (
	"context"
	"encoding/json"

	goredis "github.com/redis/go-redis/v9"

	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/realtime"
)

// Channel is the Redis pub/sub channel every replica listens on.
const Channel = "realtime:messages"

type Deliverer interface {
	Deliver(body realtime.Body)
}

type Fanout struct {
	log    logger.ILog
	client *goredis.Client
}

func New(log logger.ILog, client *goredis.Client) *Fanout {
	return &Fanout{log: log, client: client}
}

func (f *Fanout) Publish(ctx context.Context, body realtime.Body) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}

	return f.client.Publish(ctx, Channel, raw).Err()
}

// Start subscribes and returns once Redis confirmed it, so a socket accepted
// afterwards misses nothing published afterwards; delivery runs until ctx
// ends. go-redis resubscribes by itself after a dropped connection.
func (f *Fanout) Start(ctx context.Context, to Deliverer) error {
	sub := f.client.Subscribe(ctx, Channel)
	if _, err := sub.Receive(ctx); err != nil {
		_ = sub.Close()

		return err
	}

	go func() {
		defer func() { _ = sub.Close() }()
		messages := sub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case message, ok := <-messages:
				if !ok {
					return
				}
				var body realtime.Body
				if err := json.Unmarshal([]byte(message.Payload), &body); err != nil {
					f.log.Error(err)

					continue
				}
				to.Deliver(body)
			}
		}
	}()

	return nil
}
