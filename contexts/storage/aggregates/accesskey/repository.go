package accesskey

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

const columns = `id, storage_id, name, access_key_id, permission, created_by, created_at`

func (r *Repository) Insert(ctx context.Context, k *AccessKey) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		INSERT INTO access_keys (`+columns+`) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		k.ID, k.StorageID, k.Name, k.AccessKeyID, string(k.Permission), k.CreatedBy, k.CreatedAt)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

func (r *Repository) Find(ctx context.Context, storageID, id uuid.UUID) (*AccessKey, error) {
	k, err := scan(r.queries.Executor(ctx).QueryRow(ctx, `
		SELECT `+columns+` FROM access_keys WHERE id = $1 AND storage_id = $2`, id, storageID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrorNotFound
	}

	return k, err
}

func (r *Repository) ListByStorage(ctx context.Context, storageID uuid.UUID) ([]*AccessKey, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT `+columns+` FROM access_keys WHERE storage_id = $1 ORDER BY created_at, id`, storageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*AccessKey{}
	for rows.Next() {
		k, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}

	return out, rows.Err()
}

func (r *Repository) CountByStorage(ctx context.Context, storageID uuid.UUID) (int, error) {
	var n int
	err := r.queries.Executor(ctx).QueryRow(ctx, `SELECT count(*) FROM access_keys WHERE storage_id = $1`, storageID).Scan(&n)

	return n, err
}

func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `DELETE FROM access_keys WHERE id = $1`, id)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

func (r *Repository) DeleteByStorage(ctx context.Context, storageID uuid.UUID) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `DELETE FROM access_keys WHERE storage_id = $1`, storageID)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

func scan(row pgx.Row) (*AccessKey, error) {
	var k AccessKey
	var permission string
	if err := row.Scan(&k.ID, &k.StorageID, &k.Name, &k.AccessKeyID, &permission, &k.CreatedBy, &k.CreatedAt); err != nil {
		return nil, err
	}
	k.Permission = Permission(permission)

	return &k, nil
}
