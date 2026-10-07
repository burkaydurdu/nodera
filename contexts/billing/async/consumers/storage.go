package consumers

import (
	"context"

	"github.com/google/uuid"
	"github.com/piggystats/rabbitmq"
	"github.com/shopspring/decimal"

	"github.com/piggystats/nodera/contexts/billing/aggregates/usage"
	"github.com/piggystats/nodera/contexts/billing/async/constants"
	"github.com/piggystats/nodera/contexts/billing/async/events"
	"github.com/piggystats/nodera/utils/str"
)

// StoragePackageEvents records the Packages each Storage held as the storage
// context reports them: intervals named by the Storage and their start. The
// writes are idempotent and order-tolerant: a stop that arrives before its
// start is kept for it, and a redelivered or replayed start never moves the
// price.
type StoragePackageEvents struct {
	usage usage.IService
}

func NewStoragePackageEvents(usageService usage.IService) *StoragePackageEvents {
	return &StoragePackageEvents{usage: usageService}
}

func (c *StoragePackageEvents) RoutingKeys() []string {
	return []string{constants.StoragePackageStarted, constants.StoragePackageStopped}
}

func (c *StoragePackageEvents) Consume(message *rabbitmq.Message) error {
	ctx := context.Background()

	switch message.RoutingKey {
	case constants.StoragePackageStarted:
		return c.started(ctx, message)
	case constants.StoragePackageStopped:
		return c.stopped(ctx, message)
	default:
		return nil
	}
}

func (c *StoragePackageEvents) started(ctx context.Context, message *rabbitmq.Message) error {
	var event events.StoragePackageStartedEvent
	if err := decodeUsage(message, &event); err != nil {
		return err
	}
	p := event.Payload
	if p.StorageID == uuid.Nil || p.TeamID == uuid.Nil || p.ProjectID == uuid.Nil || p.StartedAt.IsZero() ||
		str.IsBlank(p.BucketName) || str.IsBlank(p.PackageCode) || p.SizeBytes <= 0 {
		return usage.ErrorMalformedEvent.Copy().SetMessage("event payload is missing the storage, team, project, start, bucket name, package or its size")
	}
	price, err := decimal.NewFromString(p.MonthlyPrice)
	if err != nil || price.IsNegative() {
		return usage.ErrorMalformedEvent.Copy().SetMessage("event payload carries no valid monthly price")
	}

	// The price is stored to the cent; a fraction of one is never given away.
	return c.usage.StartStorage(ctx, &usage.StorageInterval{
		StorageID: p.StorageID, TeamID: p.TeamID, ProjectID: p.ProjectID, ProjectName: p.ProjectName, BucketName: p.BucketName,
		Region: p.Region, PackageCode: p.PackageCode, SizeBytes: p.SizeBytes, MonthlyPrice: price.RoundCeil(2),
		StartedAt: p.StartedAt.UTC(),
	})
}

func (c *StoragePackageEvents) stopped(ctx context.Context, message *rabbitmq.Message) error {
	var event events.StoragePackageStoppedEvent
	if err := decodeUsage(message, &event); err != nil {
		return err
	}
	p := event.Payload
	if p.StorageID == uuid.Nil || p.TeamID == uuid.Nil || p.StartedAt.IsZero() || p.StoppedAt.IsZero() {
		return usage.ErrorMalformedEvent.Copy().SetMessage("event payload is missing the storage, team, start or stop")
	}
	if p.StoppedAt.Before(p.StartedAt) {
		return usage.ErrorMalformedEvent.Copy().SetMessage("event payload stops the interval before it started")
	}

	return c.usage.StopStorage(ctx, p.StorageID, p.TeamID, p.StartedAt.UTC(), p.StoppedAt.UTC())
}
