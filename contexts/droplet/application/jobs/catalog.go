package jobs

import (
	"context"
	"time"

	"github.com/piggystats/nodera/contexts/droplet/aggregates/catalog"
	"github.com/piggystats/nodera/logger"
)

// CatalogAnnouncer publishes every Region and Plan as catalog events, through
// the outbox, after each sync and on replay (docs/adr/0014). The kubernetes
// context keeps the newest copy of each by the time stamped here, so
// announcing the whole catalog again is harmless.
type CatalogAnnouncer struct {
	log      logger.ILog
	catalog  catalog.IService
	producer CatalogProducer
	now      func() time.Time
}

func NewCatalogAnnouncer(log logger.ILog, catalogService catalog.IService, producer CatalogProducer) *CatalogAnnouncer {
	return &CatalogAnnouncer{log: log, catalog: catalogService, producer: producer, now: time.Now}
}

// Announce publishes the Regions first, then the Plans, and answers how many
// events it queued. One failed publish does not stop the rest.
func (a *CatalogAnnouncer) Announce(ctx context.Context) (int, error) {
	at := a.now()
	regions, err := a.catalog.ListRegions(ctx)
	if err != nil {
		return 0, err
	}
	plans, err := a.catalog.ListPlans(ctx)
	if err != nil {
		return 0, err
	}

	var published int
	var firstErr error
	for _, r := range regions {
		if err := a.producer.PublishRegion(ctx, r, at); err != nil {
			a.log.ErrorWithFields(err, logger.NewFields().Add("region", r.Code))
			if firstErr == nil {
				firstErr = err
			}

			continue
		}
		published++
	}
	for _, p := range plans {
		if err := a.producer.PublishPlan(ctx, p, at); err != nil {
			a.log.ErrorWithFields(err, logger.NewFields().Add("plan", p.Code).Add("region", p.RegionCode))
			if firstErr == nil {
				firstErr = err
			}

			continue
		}
		published++
	}

	return published, firstErr
}
