package billingstatus

import (
	"context"
	"errors"
	"time"

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

// Find answers nil, without an error, for a Team without a row.
func (r *Repository) Find(ctx context.Context, teamID uuid.UUID) (*TeamStatus, error) {
	var s TeamStatus
	err := r.queries.Executor(ctx).QueryRow(ctx, `
		SELECT team_id, status, changed_at, open_invoice_id, suspended_at, deletion_at, next_retry_at, updated_at
		FROM team_billing_status WHERE team_id = $1`, teamID).
		Scan(&s.TeamID, &s.Status, &s.ChangedAt, &s.OpenInvoiceID, &s.SuspendedAt, &s.DeletionAt, &s.NextRetryAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}

	return &s, nil
}

// FindForUpdate locks the row for the transaction, creating the default row
// first so two concurrent transitions serialize on it.
func (r *Repository) FindForUpdate(ctx context.Context, teamID uuid.UUID, now time.Time) (*TeamStatus, error) {
	executor := r.queries.Executor(ctx)
	if _, err := executor.Exec(ctx, `
		INSERT INTO team_billing_status (team_id, status, changed_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (team_id) DO NOTHING`, teamID, NoPaymentMethod, now); err != nil {
		return nil, ErrorCannotStore.Copy().SetError(err)
	}

	var s TeamStatus
	err := executor.QueryRow(ctx, `
		SELECT team_id, status, changed_at, open_invoice_id, suspended_at, deletion_at, next_retry_at, updated_at
		FROM team_billing_status WHERE team_id = $1 FOR UPDATE`, teamID).
		Scan(&s.TeamID, &s.Status, &s.ChangedAt, &s.OpenInvoiceID, &s.SuspendedAt, &s.DeletionAt, &s.NextRetryAt, &s.UpdatedAt)
	if err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}

	return &s, nil
}

// Save writes the whole row.
func (r *Repository) Save(ctx context.Context, s *TeamStatus) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		INSERT INTO team_billing_status (team_id, status, changed_at, open_invoice_id, suspended_at, deletion_at, next_retry_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now())
		ON CONFLICT (team_id) DO UPDATE SET
			status = EXCLUDED.status, changed_at = EXCLUDED.changed_at, open_invoice_id = EXCLUDED.open_invoice_id,
			suspended_at = EXCLUDED.suspended_at, deletion_at = EXCLUDED.deletion_at, next_retry_at = EXCLUDED.next_retry_at,
			updated_at = now()`,
		s.TeamID, s.Status, s.ChangedAt, s.OpenInvoiceID, s.SuspendedAt, s.DeletionAt, s.NextRetryAt)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

// FindAll answers every row, oldest change first.
func (r *Repository) FindAll(ctx context.Context) ([]*TeamStatus, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT team_id, status, changed_at, open_invoice_id, suspended_at, deletion_at, next_retry_at, updated_at
		FROM team_billing_status ORDER BY changed_at, team_id`)
	if err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}
	defer rows.Close()

	out := make([]*TeamStatus, 0)
	for rows.Next() {
		var s TeamStatus
		if err := rows.Scan(&s.TeamID, &s.Status, &s.ChangedAt, &s.OpenInvoiceID, &s.SuspendedAt, &s.DeletionAt, &s.NextRetryAt, &s.UpdatedAt); err != nil {
			return nil, ErrorCannotRead.Copy().SetError(err)
		}
		out = append(out, &s)
	}
	if err := rows.Err(); err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}

	return out, nil
}
