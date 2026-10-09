package producers

import (
	"context"
	"time"

	"github.com/piggystats/nodera/contexts/droplet/aggregates/catalog"
	"github.com/piggystats/nodera/contexts/droplet/async/constants"
	"github.com/piggystats/nodera/contexts/droplet/async/events"
)

// CatalogChanged publishes the catalog as the kubernetes context's read model
// takes it (docs/adr/0014): every Plan and Region, each stamped with the time
// it was announced so the newest copy wins there.
type CatalogChanged struct{ client OutboxClient }

func NewCatalogChanged(client OutboxClient) *CatalogChanged {
	return &CatalogChanged{client: client}
}

func (p *CatalogChanged) PublishPlan(ctx context.Context, plan *catalog.Plan, at time.Time) error {
	return publish(ctx, p.client, constants.CatalogPlanChanged, &events.CatalogPlanChangedEvent{Payload: events.CatalogPlanChangedBody{
		Code: plan.Code, RegionCode: plan.RegionCode, CPUCount: plan.CPUCount, RAMGB: plan.RAMGB,
		HourlyPrice: plan.HourlyPrice.StringFixed(catalog.HourlyScale), MonthlyPrice: plan.MonthlyPrice().StringFixed(catalog.MonthlyScale),
		SoldOut: plan.SoldOut, Enabled: plan.Enabled, SyncedAt: at.UTC(),
	}})
}

func (p *CatalogChanged) PublishRegion(ctx context.Context, region *catalog.Region, at time.Time) error {
	return publish(ctx, p.client, constants.CatalogRegionChanged, &events.CatalogRegionChangedEvent{Payload: events.CatalogRegionChangedBody{
		Code: region.Code, City: region.City, Country: region.Country, Enabled: region.Enabled, SyncedAt: at.UTC(),
	}})
}
