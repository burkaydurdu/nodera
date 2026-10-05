package suppression

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/piggystats/nodera/contexts/mail/paging"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/utils/str"
)

const columns = `id, project_id, address, reason, message_id, created_at`

type Repository struct {
	queries *postgresql.Queries
}

func NewRepository(queries *postgresql.Queries) *Repository {
	return &Repository{queries: queries}
}

// Add is idempotent; a complaint replaces a bounce for the same address,
// never the other way round.
func (r *Repository) Add(ctx context.Context, s *Suppression) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		INSERT INTO suppressions (id, project_id, address, reason, message_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (project_id, address) DO UPDATE
		SET reason = EXCLUDED.reason, message_id = EXCLUDED.message_id
		WHERE suppressions.reason = 'bounce' AND EXCLUDED.reason = 'complaint'`,
		s.ID, s.ProjectID, s.Address, s.Reason, s.MessageID, s.CreatedAt)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

func (r *Repository) Suppressed(ctx context.Context, projectID uuid.UUID, addresses []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(addresses) == 0 {
		return out, nil
	}

	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT address FROM suppressions WHERE project_id = $1 AND address = ANY($2)`, projectID, addresses)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var address string
		if err := rows.Scan(&address); err != nil {
			return nil, err
		}
		out[address] = true
	}

	return out, rows.Err()
}

func (r *Repository) List(ctx context.Context, projectID uuid.UUID, search string, before *paging.Cursor, limit int) ([]*Suppression, *paging.Cursor, error) {
	query := `SELECT ` + columns + ` FROM suppressions WHERE project_id = $1`
	args := []any{projectID}
	if str.IsNotEmpty(search) {
		args = append(args, "%"+search+"%")
		query += fmt.Sprintf(" AND address LIKE $%d", len(args))
	}
	if before != nil {
		args = append(args, before.At, before.ID)
		query += fmt.Sprintf(" AND (created_at, id) < ($%d, $%d)", len(args)-1, len(args))
	}
	args = append(args, limit+1)
	query += fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d", len(args))

	rows, err := r.queries.Executor(ctx).Query(ctx, query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	out := []*Suppression{}
	for rows.Next() {
		s, err := scan(rows)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	var next *paging.Cursor
	if len(out) > limit {
		out = out[:limit]
		last := out[len(out)-1]
		next = &paging.Cursor{At: last.CreatedAt, ID: last.ID}
	}

	return out, next, nil
}

func (r *Repository) Find(ctx context.Context, projectID, id uuid.UUID) (*Suppression, error) {
	s, err := scan(r.queries.Executor(ctx).QueryRow(ctx, `SELECT `+columns+` FROM suppressions WHERE id = $1 AND project_id = $2`, id, projectID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrorNotFound
	}

	return s, err
}

func (r *Repository) Delete(ctx context.Context, projectID, id uuid.UUID) error {
	tag, err := r.queries.Executor(ctx).Exec(ctx, `DELETE FROM suppressions WHERE id = $1 AND project_id = $2`, id, projectID)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrorNotFound
	}

	return nil
}

func scan(row pgx.Row) (*Suppression, error) {
	var s Suppression
	if err := row.Scan(&s.ID, &s.ProjectID, &s.Address, &s.Reason, &s.MessageID, &s.CreatedAt); err != nil {
		return nil, err
	}

	return &s, nil
}
