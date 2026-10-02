package exchangerate

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/piggystats/nodera/internal/persistence/postgresql"
)

type Repository struct {
	queries *postgresql.Queries
}

func NewRepository(queries *postgresql.Queries) *Repository {
	return &Repository{queries: queries}
}

// Store writes a day's rate; a day fetched twice keeps the newer value.
func (r *Repository) Store(ctx context.Context, rate *Rate) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		INSERT INTO exchange_rates (rate_date, usd_buying, source)
		VALUES ($1, $2, $3)
		ON CONFLICT (rate_date) DO UPDATE SET usd_buying = EXCLUDED.usd_buying, source = EXCLUDED.source, fetched_at = now()`,
		Day(rate.RateDate), rate.USDBuying, rate.Source)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

// FindLatestOnOrBefore answers the rate of the day or the newest earlier one.
func (r *Repository) FindLatestOnOrBefore(ctx context.Context, date time.Time) (*Rate, error) {
	var rate Rate
	err := r.queries.Executor(ctx).QueryRow(ctx, `
		SELECT rate_date, usd_buying, source, fetched_at FROM exchange_rates
		WHERE rate_date <= $1 ORDER BY rate_date DESC LIMIT 1`, Day(date)).
		Scan(&rate.RateDate, &rate.USDBuying, &rate.Source, &rate.FetchedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrorNoRate.Copy().Add("date", Day(date).Format(time.DateOnly))
	}
	if err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}
	rate.RateDate = Day(rate.RateDate)

	return &rate, nil
}

// Exists reports whether the day already has a rate, so the fetch job stops
// asking once the day is covered.
func (r *Repository) Exists(ctx context.Context, date time.Time) (bool, error) {
	var n int
	err := r.queries.Executor(ctx).QueryRow(ctx, `SELECT count(*) FROM exchange_rates WHERE rate_date = $1`, Day(date)).Scan(&n)
	if err != nil {
		return false, ErrorCannotRead.Copy().SetError(err)
	}

	return n > 0, nil
}
