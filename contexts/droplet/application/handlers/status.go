package handlers

import (
	"context"

	"github.com/piggystats/nodera/contexts/droplet/aggregates/droplet"
	"github.com/piggystats/nodera/logger"
)

//go:generate mockgen -destination=../../mock/producers/status_changed.go -package=mockproducers github.com/piggystats/nodera/contexts/droplet/application/handlers IStatusChangedProducer
type IStatusChangedProducer interface {
	Publish(ctx context.Context, d *droplet.Droplet, from *droplet.Status) error
}

// StatusAnnouncer tells the rest of the platform that a Droplet's status
// moved. Like the Meter it runs after the commit that recorded the move
// (docs/adr/0001), so a failed publish is logged rather than returned: the
// change is already true, and the web app refetches on its next connect.
type StatusAnnouncer struct {
	log      logger.ILog
	producer IStatusChangedProducer
}

func NewStatusAnnouncer(log logger.ILog, producer IStatusChangedProducer) *StatusAnnouncer {
	return &StatusAnnouncer{log: log, producer: producer}
}

// Announce publishes when the status differs from what it was before the
// transaction; from is nil for a Droplet just created. Several moves inside
// one transaction (deleting, then deleted) go out as one, from the first
// status to the last.
func (a *StatusAnnouncer) Announce(ctx context.Context, d *droplet.Droplet, from *droplet.Status) {
	if from != nil && *from == d.Status {
		return
	}
	if err := a.producer.Publish(ctx, d, from); err != nil {
		a.log.ErrorWithFields(err, logger.NewFields().Add("droplet", d.ID.String()).Add("status", string(d.Status)))
	}
}
