package provider

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/piggystats/nodera/internal/persistence/postgresql"
)

type Repository struct {
	queries *postgresql.Queries
}

func NewRepository(queries *postgresql.Queries) *Repository {
	return &Repository{queries: queries}
}

const columns = `id, code, name, enabled, default_commission_bps, created_at, updated_at`

func (r *Repository) FindEnabled(ctx context.Context) (Providers, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT `+columns+` FROM providers WHERE enabled ORDER BY code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	providers := make(Providers, 0)
	for rows.Next() {
		p, err := scan(rows)
		if err != nil {
			return nil, err
		}
		providers = append(providers, p)
	}

	return providers, rows.Err()
}

func (r *Repository) FindByID(ctx context.Context, id uuid.UUID) (*Provider, error) {
	p, err := scan(r.queries.Executor(ctx).QueryRow(ctx, `SELECT `+columns+` FROM providers WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrorNotFound
	}

	return p, err
}

func (r *Repository) FindByCode(ctx context.Context, code string) (*Provider, error) {
	p, err := scan(r.queries.Executor(ctx).QueryRow(ctx, `SELECT `+columns+` FROM providers WHERE code = $1`, code))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrorNotFound
	}

	return p, err
}

func scan(row pgx.Row) (*Provider, error) {
	var p Provider
	if err := row.Scan(&p.ID, &p.Code, &p.Name, &p.Enabled, &p.DefaultCommissionBps, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}

	return &p, nil
}
