package sshkey

import (
	"context"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/internal/persistence/postgresql"
)

const (
	nameUniqueConstraint        = "ssh_keys_team_name_unique"
	fingerprintUniqueConstraint = "ssh_keys_team_fingerprint_unique"
)

type Repository struct {
	queries *postgresql.Queries
}

func NewRepository(queries *postgresql.Queries) *Repository {
	return &Repository{queries: queries}
}

func (r *Repository) Create(ctx context.Context, key *SSHKey) error {
	if key.ID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return ErrorCannotCreate.Copy().SetError(err)
		}
		key.ID = id
	}

	err := r.queries.Executor(ctx).QueryRow(ctx, `
		INSERT INTO ssh_keys (id, team_id, name, public_key, fingerprint, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING created_at`,
		key.ID, key.TeamID, key.Name, key.PublicKey, key.Fingerprint, key.CreatedBy,
	).Scan(&key.CreatedAt)
	if err != nil {
		if constraint, ok := postgresql.UniqueViolationConstraint(err); ok {
			switch constraint {
			case nameUniqueConstraint:
				return ErrorNameTaken
			case fingerprintUniqueConstraint:
				return ErrorFingerprintTaken
			}
		}

		return ErrorCannotCreate.Copy().SetError(err)
	}

	return nil
}

func (r *Repository) FindByTeamID(ctx context.Context, teamID uuid.UUID) (SSHKeys, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT id, team_id, name, public_key, fingerprint, created_by, created_at
		FROM ssh_keys
		WHERE team_id = $1
		ORDER BY created_at`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	keys := make(SSHKeys, 0)
	for rows.Next() {
		var k SSHKey
		if err := rows.Scan(&k.ID, &k.TeamID, &k.Name, &k.PublicKey, &k.Fingerprint, &k.CreatedBy, &k.CreatedAt); err != nil {
			return nil, err
		}
		keys = append(keys, &k)
	}

	return keys, rows.Err()
}

// Delete is scoped by Team so a key id learned elsewhere cannot be removed
// through another Team.
func (r *Repository) Delete(ctx context.Context, teamID, keyID uuid.UUID) error {
	tag, err := r.queries.Executor(ctx).Exec(ctx, `
		DELETE FROM ssh_keys WHERE team_id = $1 AND id = $2`, teamID, keyID)
	if err != nil {
		return ErrorCannotDelete.Copy().SetError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrorNotFound
	}

	return nil
}
