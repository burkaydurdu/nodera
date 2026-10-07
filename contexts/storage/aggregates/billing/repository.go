package billing

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

// Upsert is last-writer-wins on the event's own time, so a redelivered or
// overtaken event changes nothing. It answers whether the row now carries
// this event, which is when the caller may act on it.
func (r *Repository) Upsert(ctx context.Context, tb *TeamBilling) (bool, error) {
	tag, err := r.queries.Executor(ctx).Exec(ctx, `
		INSERT INTO team_billing (team_id, status, changed_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (team_id) DO UPDATE
		SET status = EXCLUDED.status, changed_at = EXCLUDED.changed_at
		WHERE team_billing.changed_at <= EXCLUDED.changed_at`, tb.TeamID, string(tb.Status), tb.ChangedAt)
	if err != nil {
		return false, ErrorCannotStore.Copy().SetError(err)
	}

	return tag.RowsAffected() > 0, nil
}

// Find answers nil, without an error, for a Team never heard about.
func (r *Repository) Find(ctx context.Context, teamID uuid.UUID) (*TeamBilling, error) {
	tb := TeamBilling{TeamID: teamID}
	var status string
	err := r.queries.Executor(ctx).QueryRow(ctx, `
		SELECT status, changed_at FROM team_billing WHERE team_id = $1`, teamID).Scan(&status, &tb.ChangedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	tb.Status = Status(status)

	return &tb, nil
}
