package taxrule

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

// Lookup picks the most specific rule valid at `at`: a named country beats
// the wildcard, then a named account type, then the newest valid-from.
func (r *Repository) Lookup(ctx context.Context, country, accountType string, at time.Time) (*TaxRule, error) {
	var rule TaxRule
	err := r.queries.Executor(ctx).QueryRow(ctx, `
		SELECT id, country, account_type, rate_bps, label_key, valid_from, created_at
		FROM tax_rules
		WHERE (country = $1 OR country = $3) AND (account_type = $2 OR account_type = $3) AND valid_from <= $4
		ORDER BY (country = $1) DESC, (account_type = $2) DESC, valid_from DESC
		LIMIT 1`, country, accountType, Any, at).
		Scan(&rule.ID, &rule.Country, &rule.AccountType, &rule.RateBps, &rule.LabelKey, &rule.ValidFrom, &rule.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrorNoRule.Copy().Add("country", country).Add("account_type", accountType)
	}
	if err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}

	return &rule, nil
}
