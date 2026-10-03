package handlers_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/piggystats/nodera/contexts/droplet/aggregates/droplet"
	"github.com/piggystats/nodera/contexts/droplet/application/handlers"
	mockproducers "github.com/piggystats/nodera/contexts/droplet/mock/producers"
	"github.com/piggystats/nodera/logger"
)

func status(s droplet.Status) *droplet.Status {
	return &s
}

func TestAnnounce_AStatusThatDidNotMoveIsNotAnnounced(t *testing.T) {
	producer := mockproducers.NewMockIStatusChangedProducer(gomock.NewController(t))
	d := &droplet.Droplet{ID: uuid.New(), Status: droplet.StatusActive}

	handlers.NewStatusAnnouncer(logger.Noop(), producer).Announce(context.Background(), d, status(droplet.StatusActive))
}

func TestAnnounce_ANewDropletIsAnnouncedFromNowhere(t *testing.T) {
	producer := mockproducers.NewMockIStatusChangedProducer(gomock.NewController(t))
	d := &droplet.Droplet{ID: uuid.New(), Status: droplet.StatusProvisioning}
	producer.EXPECT().Publish(gomock.Any(), d, (*droplet.Status)(nil)).Return(nil)

	handlers.NewStatusAnnouncer(logger.Noop(), producer).Announce(context.Background(), d, nil)
}

func TestAnnounce_AFailedPublishIsLoggedBecauseTheMoveIsAlreadyCommitted(t *testing.T) {
	producer := mockproducers.NewMockIStatusChangedProducer(gomock.NewController(t))
	d := &droplet.Droplet{ID: uuid.New(), Status: droplet.StatusFailed}
	producer.EXPECT().Publish(gomock.Any(), d, status(droplet.StatusProvisioning)).Return(errors.New("outbox unavailable"))

	handlers.NewStatusAnnouncer(logger.Noop(), producer).Announce(context.Background(), d, status(droplet.StatusProvisioning))
}
