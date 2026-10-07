package catalog

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/piggystats/nodera/internal/persistence/postgresql"
)

type Repository struct {
	queries *postgresql.Queries
}

func NewRepository(queries *postgresql.Queries) *Repository {
	return &Repository{queries: queries}
}

func (r *Repository) ListRegions(ctx context.Context) ([]*Region, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT code, city, country FROM regions WHERE active ORDER BY code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*Region{}
	for rows.Next() {
		var region Region
		if err := rows.Scan(&region.Code, &region.City, &region.Country); err != nil {
			return nil, err
		}
		out = append(out, &region)
	}

	return out, rows.Err()
}

// FindRegion answers only a Region Storage is still opened in.
func (r *Repository) FindRegion(ctx context.Context, code string) (*Region, error) {
	region := Region{Code: code}
	err := r.queries.Executor(ctx).QueryRow(ctx, `
		SELECT city, country FROM regions WHERE code = $1 AND active`, code).Scan(&region.City, &region.Country)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrorUnknownRegion
	}
	if err != nil {
		return nil, err
	}

	return &region, nil
}

func (r *Repository) ListPackages(ctx context.Context) ([]*Package, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT code, size_bytes, monthly_price FROM packages WHERE active ORDER BY size_bytes`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*Package{}
	for rows.Next() {
		var p Package
		if err := rows.Scan(&p.Code, &p.SizeBytes, &p.MonthlyPrice); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}

	return out, rows.Err()
}

// FindPackage answers only a Package that is still sold.
func (r *Repository) FindPackage(ctx context.Context, code string) (*Package, error) {
	p := Package{Code: code}
	err := r.queries.Executor(ctx).QueryRow(ctx, `
		SELECT size_bytes, monthly_price FROM packages WHERE code = $1 AND active`, code).Scan(&p.SizeBytes, &p.MonthlyPrice)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrorUnknownPackage
	}
	if err != nil {
		return nil, err
	}

	return &p, nil
}
