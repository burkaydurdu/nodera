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

// MailPackageEvents records the Packages each Project held as the mail
// context reports them: intervals named by the Project and their start. The
// writes are idempotent and order-tolerant: a stop that arrives before its
// start is kept for it, and a redelivered or replayed start never moves the
// price.
type MailPackageEvents struct {
	usage usage.IService
}

func NewMailPackageEvents(usageService usage.IService) *MailPackageEvents {
	return &MailPackageEvents{usage: usageService}
}

func (c *MailPackageEvents) RoutingKeys() []string {
	return []string{constants.MailPackageStarted, constants.MailPackageStopped}
}

func (c *MailPackageEvents) Consume(message *rabbitmq.Message) error {
	ctx := context.Background()

	switch message.RoutingKey {
	case constants.MailPackageStarted:
		return c.started(ctx, message)
	case constants.MailPackageStopped:
		return c.stopped(ctx, message)
	default:
		return nil
	}
}

func (c *MailPackageEvents) started(ctx context.Context, message *rabbitmq.Message) error {
	var event events.MailPackageStartedEvent
	if err := decodeUsage(message, &event); err != nil {
		return err
	}
	p := event.Payload
	if p.ProjectID == uuid.Nil || p.TeamID == uuid.Nil || p.StartedAt.IsZero() || str.IsBlank(p.PackageCode) || p.MonthlyEmails <= 0 {
		return usage.ErrorMalformedEvent.Copy().SetMessage("event payload is missing the project, team, start, package or its emails")
	}
	price, err := decimal.NewFromString(p.MonthlyPrice)
	if err != nil || price.IsNegative() {
		return usage.ErrorMalformedEvent.Copy().SetMessage("event payload carries no valid monthly price")
	}

	// The price is stored to the cent; a fraction of one is never given away.
	return c.usage.StartMail(ctx, &usage.MailInterval{
		ProjectID: p.ProjectID, TeamID: p.TeamID, ProjectName: p.ProjectName, PackageCode: p.PackageCode,
		MonthlyEmails: p.MonthlyEmails, MonthlyPrice: price.RoundCeil(2), StartedAt: p.StartedAt.UTC(),
	})
}

func (c *MailPackageEvents) stopped(ctx context.Context, message *rabbitmq.Message) error {
	var event events.MailPackageStoppedEvent
	if err := decodeUsage(message, &event); err != nil {
		return err
	}
	p := event.Payload
	if p.ProjectID == uuid.Nil || p.TeamID == uuid.Nil || p.StartedAt.IsZero() || p.StoppedAt.IsZero() {
		return usage.ErrorMalformedEvent.Copy().SetMessage("event payload is missing the project, team, start or stop")
	}
	if p.StoppedAt.Before(p.StartedAt) {
		return usage.ErrorMalformedEvent.Copy().SetMessage("event payload stops the interval before it started")
	}

	return c.usage.StopMail(ctx, p.ProjectID, p.TeamID, p.StartedAt.UTC(), p.StoppedAt.UTC())
}
