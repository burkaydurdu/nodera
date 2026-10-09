package jobs

import (
	"context"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/droplet/aggregates/catalog"
	"github.com/piggystats/nodera/logger"
)

// unmappedOffersSample bounds one log line when a Provider lists far more
// than Nodera sells.
const unmappedOffersSample = 50

// SyncCatalog upserts every enabled Provider's Offers, marks the ones the
// Provider stopped listing as out of stock, and reprices every mapped Plan
// from its Offer and Commission. A Provider that cannot be reached is skipped
// whole: its catalog stays as it was rather than going sold out.
func (j *Jobs) SyncCatalog(ctx context.Context) error {
	enabled, err := j.providers.FindEnabled(ctx)
	if err != nil {
		return err
	}

	var firstErr error
	for _, p := range enabled {
		if err := j.syncProvider(ctx, p.ID, p.Code); err != nil {
			j.log.ErrorWithFields(err, logger.NewFields().Add("provider", p.Code))
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	// The catalog goes out as it stands after the sync, Providers that could
	// not be reached included: their Plans are as they were, which is still
	// the truth the kubernetes context should hold.
	if _, err := j.announcer.Announce(ctx); err != nil {
		j.log.Error(err)
		if firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

func (j *Jobs) syncProvider(ctx context.Context, providerID uuid.UUID, code string) error {
	adapter, err := j.registry.Get(code)
	if err != nil {
		return err
	}

	livePlans, err := adapter.ListPlans(ctx)
	if err != nil {
		return err
	}

	seen := make([]uuid.UUID, 0, len(livePlans))
	for _, lp := range livePlans {
		for _, region := range lp.Regions {
			offer := &catalog.Offer{
				ProviderID:  providerID,
				PlanRef:     lp.Ref,
				RegionRef:   region.RegionRef,
				HourlyPrice: region.HourlyPrice,
				Stock:       region.Stock,
				Hardware:    lp.Hardware,
			}
			if err := j.catalog.UpsertOffer(ctx, offer); err != nil {
				return err
			}
			seen = append(seen, offer.ID)
		}
	}

	// An answer with nothing hourly-priced is a broken answer, not an empty
	// catalog: expiring every Offer on it would sell the whole catalog out.
	if len(seen) == 0 {
		j.log.ErrorWithFields(catalog.ErrorCannotSync.Copy().SetMessage("provider listed no hourly-priced plans; its offers were left as they were"),
			logger.NewFields().Add("provider", code).Add("plans_listed", len(livePlans)))

		return nil
	}

	vanished, err := j.catalog.ExpireOffersExcept(ctx, providerID, seen)
	if err != nil {
		return err
	}
	if vanished > 0 {
		j.log.WarnWithFields("offers vanished from the provider", logger.NewFields().Add("provider", code).Add("count", vanished))
	}

	if err := j.repricePlans(ctx, providerID, code); err != nil {
		return err
	}

	return j.reportUnmappedOffers(ctx, providerID, code)
}

// reportUnmappedOffers names the in-stock Offers no Plan is built on, so an
// operator can spot a plan slug that changed upstream or a Provider product
// Nodera could sell.
func (j *Jobs) reportUnmappedOffers(ctx context.Context, providerID uuid.UUID, code string) error {
	offers, err := j.catalog.ListUnmappedOffersByProvider(ctx, providerID)
	if err != nil {
		return err
	}
	if len(offers) == 0 {
		return nil
	}

	shown := min(len(offers), unmappedOffersSample)
	sample := make([]string, 0, shown)
	for _, o := range offers[:shown] {
		sample = append(sample, o.PlanRef+"@"+o.RegionRef+"="+o.HourlyPrice.StringFixed(catalog.HourlyScale))
	}
	j.log.InfoWithFields("offers without a plan", logger.NewFields().
		Add("provider", code).Add("count", len(offers)).Add("offers", sample))

	return nil
}

func (j *Jobs) repricePlans(ctx context.Context, providerID uuid.UUID, code string) error {
	plans, err := j.catalog.ListMappedPlansByProvider(ctx, providerID)
	if err != nil {
		return err
	}

	for _, plan := range plans {
		price := catalog.HourlyPrice(plan.Offer.HourlyPrice, plan.CommissionBps())
		soldOut := plan.Offer.Stock == 0

		if price.Equal(plan.HourlyPrice) && soldOut == plan.SoldOut {
			continue
		}

		fields := logger.NewFields().Add("provider", code).Add("plan", plan.Code).Add("region", plan.RegionCode)
		if !price.Equal(plan.HourlyPrice) {
			j.log.InfoWithFields("plan price changed", fields.
				Add("old_hourly", plan.HourlyPrice.StringFixed(catalog.HourlyScale)).
				Add("new_hourly", price.StringFixed(catalog.HourlyScale)))
		}
		if soldOut != plan.SoldOut {
			j.log.InfoWithFields("plan availability changed", fields.Add("sold_out", soldOut))
		}

		if err := j.catalog.UpdatePlanPricing(ctx, plan.ID, price, soldOut); err != nil {
			return err
		}
	}

	return nil
}
