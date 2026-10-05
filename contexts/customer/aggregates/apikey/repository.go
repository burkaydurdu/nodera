package apikey

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/piggystats/nodera/internal/persistence/postgresql"
	"github.com/piggystats/nodera/pkg/authz"
)

type Repository struct {
	queries *postgresql.Queries
}

func NewRepository(queries *postgresql.Queries) *Repository {
	return &Repository{queries: queries}
}

const keyColumns = `id, project_id, team_id, name, secret_hash, permissions, created_by, created_at, revoked_at`

func (r *Repository) Create(ctx context.Context, key *APIKey) error {
	err := r.queries.Executor(ctx).QueryRow(ctx, `
		INSERT INTO api_keys (id, project_id, team_id, name, secret_hash, permissions, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at`,
		key.ID, key.ProjectID, key.TeamID, key.Name, key.SecretHash, permissionNames(key.Permissions), key.CreatedBy,
	).Scan(&key.CreatedAt)
	if err != nil {
		return ErrorCannotCreate.Copy().SetError(err)
	}

	return nil
}

func (r *Repository) CountActive(ctx context.Context, projectID uuid.UUID) (int, error) {
	var count int
	err := r.queries.Executor(ctx).QueryRow(ctx, `
		SELECT count(*) FROM api_keys WHERE project_id = $1 AND revoked_at IS NULL`, projectID,
	).Scan(&count)

	return count, err
}

func (r *Repository) FindActiveByProjectID(ctx context.Context, projectID uuid.UUID) (APIKeys, error) {
	return r.list(ctx, `
		SELECT `+keyColumns+`
		FROM api_keys
		WHERE project_id = $1 AND revoked_at IS NULL
		ORDER BY created_at DESC, id`, projectID)
}

// FindAllActive and FindAllRevoked feed the rewrite of the Redis entries; they
// walk every row on purpose.
func (r *Repository) FindAllActive(ctx context.Context) (APIKeys, error) {
	return r.list(ctx, `SELECT `+keyColumns+` FROM api_keys WHERE revoked_at IS NULL ORDER BY created_at`)
}

func (r *Repository) FindAllRevoked(ctx context.Context) (APIKeys, error) {
	return r.list(ctx, `SELECT `+keyColumns+` FROM api_keys WHERE revoked_at IS NOT NULL ORDER BY created_at`)
}

// Revoke is scoped by Team and Project so an id learned elsewhere cannot end a
// key through another Project. A key already revoked is not found.
func (r *Repository) Revoke(ctx context.Context, teamID, projectID uuid.UUID, id string) error {
	tag, err := r.queries.Executor(ctx).Exec(ctx, `
		UPDATE api_keys SET revoked_at = now()
		WHERE team_id = $1 AND project_id = $2 AND id = $3 AND revoked_at IS NULL`, teamID, projectID, id)
	if err != nil {
		return ErrorCannotRevoke.Copy().SetError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrorNotFound
	}

	return nil
}

func (r *Repository) list(ctx context.Context, query string, args ...any) (APIKeys, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	keys := make(APIKeys, 0)
	for rows.Next() {
		key, err := scan(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}

	return keys, rows.Err()
}

func scan(row pgx.Row) (*APIKey, error) {
	var (
		k           APIKey
		permissions []string
	)
	err := row.Scan(&k.ID, &k.ProjectID, &k.TeamID, &k.Name, &k.SecretHash, &permissions, &k.CreatedBy, &k.CreatedAt, &k.RevokedAt)
	if err != nil {
		return nil, err
	}
	k.Permissions = make([]authz.Permission, 0, len(permissions))
	for _, p := range permissions {
		k.Permissions = append(k.Permissions, authz.Permission(p))
	}

	return &k, nil
}

func permissionNames(permissions []authz.Permission) []string {
	names := make([]string, 0, len(permissions))
	for _, p := range permissions {
		names = append(names, string(p))
	}

	return names
}
