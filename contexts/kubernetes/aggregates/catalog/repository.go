package catalog

import (
	"context"
	"errors"

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

// The upserts are last-writer-wins on the event's own time, so a redelivered
// or overtaken announcement changes nothing.

func (r *Repository) UpsertRegion(ctx context.Context, region *Region) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		INSERT INTO catalog_regions (code, city, country, enabled, synced_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (code) DO UPDATE
		SET city = EXCLUDED.city, country = EXCLUDED.country, enabled = EXCLUDED.enabled, synced_at = EXCLUDED.synced_at
		WHERE catalog_regions.synced_at <= EXCLUDED.synced_at`,
		region.Code, region.City, region.Country, region.Enabled, region.SyncedAt)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

func (r *Repository) UpsertPlan(ctx context.Context, p *Plan) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		INSERT INTO catalog_plans (code, region_code, cpu_count, ram_gb, hourly_price, monthly_price, sold_out, enabled, synced_at)
		VALUES ($1, $2, $3, $4, $5::numeric, $6::numeric, $7, $8, $9)
		ON CONFLICT (code, region_code) DO UPDATE
		SET cpu_count = EXCLUDED.cpu_count, ram_gb = EXCLUDED.ram_gb, hourly_price = EXCLUDED.hourly_price,
		    monthly_price = EXCLUDED.monthly_price, sold_out = EXCLUDED.sold_out, enabled = EXCLUDED.enabled,
		    synced_at = EXCLUDED.synced_at
		WHERE catalog_plans.synced_at <= EXCLUDED.synced_at`,
		p.Code, p.RegionCode, p.CPUCount, p.RAMGB, p.HourlyPrice.StringFixed(HourlyScale), p.MonthlyPrice.StringFixed(MonthlyScale),
		p.SoldOut, p.Enabled, p.SyncedAt)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

// FindRegion answers an enabled Region by its code.
func (r *Repository) FindRegion(ctx context.Context, code string) (*Region, error) {
	region := Region{Code: code}
	err := r.queries.Executor(ctx).QueryRow(ctx, `
		SELECT city, country, enabled, synced_at FROM catalog_regions WHERE code = $1 AND enabled`, code).
		Scan(&region.City, &region.Country, &region.Enabled, &region.SyncedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrorRegionNotFound
	}
	if err != nil {
		return nil, err
	}

	return &region, nil
}

const planColumns = `code, region_code, cpu_count, ram_gb, hourly_price::text, monthly_price::text, sold_out, enabled, synced_at`

// FindPlan answers an enabled Plan of a Region, whether or not it fits a Node;
// the caller says which refusal applies.
func (r *Repository) FindPlan(ctx context.Context, code, regionCode string) (*Plan, error) {
	p, err := scanPlan(r.queries.Executor(ctx).QueryRow(ctx, `
		SELECT `+planColumns+` FROM catalog_plans WHERE code = $1 AND region_code = $2 AND enabled`, code, regionCode))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrorPlanNotFound
	}

	return p, err
}

// ListNodePlans answers the enabled Plans of a Region large enough to be a
// Node, cheapest first; sold-out ones stay listed and flagged.
func (r *Repository) ListNodePlans(ctx context.Context, regionCode string) ([]*Plan, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT `+planColumns+` FROM catalog_plans
		WHERE region_code = $1 AND enabled AND cpu_count >= $2 AND ram_gb >= $3
		ORDER BY hourly_price, code`, regionCode, MinNodeCPUs, MinNodeRAMGB)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*Plan{}
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}

	return out, rows.Err()
}

// ListOfferedVersions answers the versions a Cluster may be opened with,
// newest first.
func (r *Repository) ListOfferedVersions(ctx context.Context) ([]*Version, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT version, offered, created_at FROM kubernetes_versions WHERE offered
		ORDER BY string_to_array(version, '.')::int[] DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*Version{}
	for rows.Next() {
		var v Version
		if err := rows.Scan(&v.Version, &v.Offered, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &v)
	}

	return out, rows.Err()
}

func (r *Repository) FindOfferedVersion(ctx context.Context, version string) (*Version, error) {
	v := Version{Version: version}
	err := r.queries.Executor(ctx).QueryRow(ctx, `
		SELECT offered, created_at FROM kubernetes_versions WHERE version = $1 AND offered`, version).Scan(&v.Offered, &v.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrorVersionNotOffered
	}
	if err != nil {
		return nil, err
	}

	return &v, nil
}

func scanPlan(row pgx.Row) (*Plan, error) {
	var p Plan
	var hourly, monthly string
	if err := row.Scan(&p.Code, &p.RegionCode, &p.CPUCount, &p.RAMGB, &hourly, &monthly, &p.SoldOut, &p.Enabled, &p.SyncedAt); err != nil {
		return nil, err
	}
	// Prices travel as text: numeric never becomes a float on the way out.
	var err error
	if p.HourlyPrice, err = decimal.NewFromString(hourly); err != nil {
		return nil, err
	}
	if p.MonthlyPrice, err = decimal.NewFromString(monthly); err != nil {
		return nil, err
	}

	return &p, nil
}
