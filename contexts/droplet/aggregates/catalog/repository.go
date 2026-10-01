package catalog

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/piggystats/nodera/internal/persistence/postgresql"
)

type Repository struct {
	queries *postgresql.Queries
}

func NewRepository(queries *postgresql.Queries) *Repository {
	return &Repository{queries: queries}
}

// ---------------------------------------------------------------------------
// Regions and Images
// ---------------------------------------------------------------------------

func (r *Repository) ListEnabledRegions(ctx context.Context) (Regions, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT r.id, r.code, r.city, r.country, r.enabled,
		       (SELECT count(*) FROM plans p WHERE p.region_id = r.id AND p.enabled)
		FROM regions r
		WHERE r.enabled
		ORDER BY r.code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	regions := make(Regions, 0)
	for rows.Next() {
		var region Region
		if err := rows.Scan(&region.ID, &region.Code, &region.City, &region.Country, &region.Enabled, &region.PlanCount); err != nil {
			return nil, err
		}
		regions = append(regions, &region)
	}

	return regions, rows.Err()
}

func (r *Repository) FindRegionByCode(ctx context.Context, code string) (*Region, error) {
	var region Region
	err := r.queries.Executor(ctx).QueryRow(ctx, `
		SELECT id, code, city, country, enabled FROM regions WHERE code = $1 AND enabled`, code).
		Scan(&region.ID, &region.Code, &region.City, &region.Country, &region.Enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrorRegionNotFound
	}
	if err != nil {
		return nil, err
	}

	return &region, nil
}

func (r *Repository) ListEnabledImages(ctx context.Context) (Images, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT id, code, name, family, version, enabled FROM images WHERE enabled ORDER BY code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	images := make(Images, 0)
	for rows.Next() {
		var image Image
		if err := rows.Scan(&image.ID, &image.Code, &image.Name, &image.Family, &image.Version, &image.Enabled); err != nil {
			return nil, err
		}
		images = append(images, &image)
	}

	return images, rows.Err()
}

// FindImageMapping answers an enabled Image and the reference the given
// Provider knows it by; an Image the Provider has no mapping for is not found.
func (r *Repository) FindImageMapping(ctx context.Context, code string, providerID uuid.UUID) (*ImageMapping, error) {
	var m ImageMapping
	err := r.queries.Executor(ctx).QueryRow(ctx, `
		SELECT i.id, i.code, i.name, i.family, i.version, i.enabled, pi.provider_id, pi.provider_ref
		FROM images i
		JOIN provider_images pi ON pi.image_id = i.id AND pi.provider_id = $2
		WHERE i.code = $1 AND i.enabled`, code, providerID).
		Scan(&m.ID, &m.Code, &m.Name, &m.Family, &m.Version, &m.Enabled, &m.ProviderID, &m.ProviderRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrorImageNotFound
	}
	if err != nil {
		return nil, err
	}

	return &m, nil
}

// ---------------------------------------------------------------------------
// Plans
// ---------------------------------------------------------------------------

// Prices travel as text: numeric never becomes a float on the way in or out.
const planColumns = `
	p.id, p.code, p.region_id, r.code, p.cpu_count, p.cpu_description, p.ram_gb, p.disk_description,
	p.bandwidth_description, p.commission_bps_override, p.hourly_price::text, p.monthly_cap_hours, p.sold_out, p.enabled,
	o.id, o.provider_id, o.provider_plan_ref, o.provider_region_ref, o.hourly_price::text, o.stock, o.hardware, o.synced_at,
	pr.code, pr.default_commission_bps,
	COALESCE((
		SELECT array_agg(i.code ORDER BY i.code)
		FROM provider_images pi JOIN images i ON i.id = pi.image_id AND i.enabled
		WHERE pi.provider_id = o.provider_id
	), '{}'::text[])`

const planFrom = `
	FROM plans p
	JOIN regions r ON r.id = p.region_id
	JOIN offers o ON o.id = p.offer_id
	JOIN providers pr ON pr.id = o.provider_id`

func (r *Repository) ListEnabledPlansByRegion(ctx context.Context, regionID uuid.UUID) (Plans, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `SELECT `+planColumns+planFrom+`
		WHERE p.region_id = $1 AND p.enabled
		ORDER BY p.hourly_price, p.code`, regionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanPlans(rows)
}

// FindPlanForSale is the Plan a customer may buy: enabled, in an enabled
// Region, addressed by the codes the catalog showed.
func (r *Repository) FindPlanForSale(ctx context.Context, code, regionCode string) (*Plan, error) {
	p, err := scanPlan(r.queries.Executor(ctx).QueryRow(ctx, `SELECT `+planColumns+planFrom+`
		WHERE p.code = $1 AND r.code = $2 AND p.enabled AND r.enabled`, code, regionCode))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrorPlanNotFound
	}

	return p, err
}

// ListMappedPlansByProvider answers every Plan backed by one of the Provider's
// Offers, enabled or not, so the sync reprices all of them.
func (r *Repository) ListMappedPlansByProvider(ctx context.Context, providerID uuid.UUID) (Plans, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `SELECT `+planColumns+planFrom+`
		WHERE o.provider_id = $1
		ORDER BY p.code, r.code`, providerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanPlans(rows)
}

func (r *Repository) UpdatePlanPricing(ctx context.Context, planID uuid.UUID, hourly decimal.Decimal, soldOut bool) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		UPDATE plans SET hourly_price = $2::numeric, sold_out = $3, updated_at = now() WHERE id = $1`,
		planID, hourly.StringFixed(HourlyScale), soldOut)
	if err != nil {
		return ErrorCannotUpdate.Copy().SetError(err)
	}

	return nil
}

func (r *Repository) MarkPlanSoldOut(ctx context.Context, planID uuid.UUID) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		UPDATE plans SET sold_out = true, updated_at = now() WHERE id = $1`, planID)
	if err != nil {
		return ErrorCannotUpdate.Copy().SetError(err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Offers
// ---------------------------------------------------------------------------

// UpsertOffer records what the Provider reports and stamps synced_at, so an
// Offer the Provider stopped listing is the one whose stamp falls behind.
func (r *Repository) UpsertOffer(ctx context.Context, offer *Offer) error {
	hardware, err := json.Marshal(offer.Hardware)
	if err != nil {
		return ErrorCannotSync.Copy().SetError(err)
	}
	if offer.Hardware == nil {
		hardware = []byte(`{}`)
	}

	err = r.queries.Executor(ctx).QueryRow(ctx, `
		INSERT INTO offers (provider_id, provider_plan_ref, provider_region_ref, hourly_price, stock, hardware, synced_at)
		VALUES ($1, $2, $3, $4::numeric, $5, $6, now())
		ON CONFLICT (provider_id, provider_plan_ref, provider_region_ref) DO UPDATE
			SET hourly_price = EXCLUDED.hourly_price, stock = EXCLUDED.stock, hardware = EXCLUDED.hardware, synced_at = now()
		RETURNING id, synced_at`,
		offer.ProviderID, offer.PlanRef, offer.RegionRef, offer.HourlyPrice.StringFixed(HourlyScale), offer.Stock, hardware,
	).Scan(&offer.ID, &offer.SyncedAt)
	if err != nil {
		return ErrorCannotSync.Copy().SetError(err)
	}

	return nil
}

// ExpireOffersExcept zeroes the stock of the Provider's Offers the last fetch
// did not mention: a vanished Offer is sold out, never deleted. Matching on
// the ids seen keeps the check independent of clock skew between the service
// and the database.
func (r *Repository) ExpireOffersExcept(ctx context.Context, providerID uuid.UUID, seen []uuid.UUID) (int64, error) {
	tag, err := r.queries.Executor(ctx).Exec(ctx, `
		UPDATE offers SET stock = 0 WHERE provider_id = $1 AND NOT (id = ANY($2)) AND stock <> 0`, providerID, seen)
	if err != nil {
		return 0, ErrorCannotSync.Copy().SetError(err)
	}

	return tag.RowsAffected(), nil
}

// ListUnmappedOffersByProvider answers the Provider's in-stock Offers no Plan
// is built on: what the Provider sells that Nodera does not, or a mapping
// that broke when a plan slug changed upstream.
func (r *Repository) ListUnmappedOffersByProvider(ctx context.Context, providerID uuid.UUID) (Offers, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT o.id, o.provider_id, o.provider_plan_ref, o.provider_region_ref, o.hourly_price::text, o.stock, o.synced_at
		FROM offers o
		WHERE o.provider_id = $1 AND o.stock > 0
		  AND NOT EXISTS (SELECT 1 FROM plans p WHERE p.offer_id = o.id)
		ORDER BY o.provider_plan_ref, o.provider_region_ref`, providerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	offers := make(Offers, 0)
	for rows.Next() {
		var (
			o     Offer
			price string
		)
		if err := rows.Scan(&o.ID, &o.ProviderID, &o.PlanRef, &o.RegionRef, &price, &o.Stock, &o.SyncedAt); err != nil {
			return nil, err
		}
		if o.HourlyPrice, err = decimal.NewFromString(price); err != nil {
			return nil, err
		}
		offers = append(offers, &o)
	}

	return offers, rows.Err()
}

func (r *Repository) UpdateOffer(ctx context.Context, offerID uuid.UUID, hourly decimal.Decimal, stock int) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		UPDATE offers SET hourly_price = $2::numeric, stock = $3, synced_at = now() WHERE id = $1`,
		offerID, hourly.StringFixed(HourlyScale), stock)
	if err != nil {
		return ErrorCannotUpdate.Copy().SetError(err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Scanning
// ---------------------------------------------------------------------------

func scanPlans(rows pgx.Rows) (Plans, error) {
	plans := make(Plans, 0)
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		plans = append(plans, p)
	}

	return plans, rows.Err()
}

func scanPlan(row pgx.Row) (*Plan, error) {
	var (
		p                  Plan
		planPrice          string
		offerPrice         string
		hardware           []byte
		images             []string
		commissionOverride *int
	)
	err := row.Scan(
		&p.ID, &p.Code, &p.RegionID, &p.RegionCode, &p.CPUCount, &p.CPUDescription, &p.RAMGB, &p.DiskDescription,
		&p.BandwidthDescription, &commissionOverride, &planPrice, &p.MonthlyCapHours, &p.SoldOut, &p.Enabled,
		&p.Offer.ID, &p.Offer.ProviderID, &p.Offer.PlanRef, &p.Offer.RegionRef, &offerPrice, &p.Offer.Stock, &hardware, &p.Offer.SyncedAt,
		&p.ProviderCode, &p.ProviderDefaultBps,
		&images,
	)
	if err != nil {
		return nil, err
	}

	p.CommissionBpsOverride = commissionOverride
	if p.HourlyPrice, err = decimal.NewFromString(planPrice); err != nil {
		return nil, err
	}
	if p.Offer.HourlyPrice, err = decimal.NewFromString(offerPrice); err != nil {
		return nil, err
	}
	if len(hardware) > 0 {
		if err := json.Unmarshal(hardware, &p.Offer.Hardware); err != nil {
			return nil, err
		}
	}
	p.Images = images

	return &p, nil
}
