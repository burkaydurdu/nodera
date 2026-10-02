package threshold

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

func (r *Repository) Find(ctx context.Context, kind Kind) (*Threshold, error) {
	t := Threshold{Kind: kind}
	err := r.queries.Executor(ctx).QueryRow(ctx, `SELECT amount FROM thresholds WHERE kind = $1`, kind).Scan(&t.Amount)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrorMissing.Copy().Add("kind", string(kind))
	}
	if err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}

	return &t, nil
}
