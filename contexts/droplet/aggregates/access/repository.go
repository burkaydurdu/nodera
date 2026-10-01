package access

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

// The upserts are idempotent so a redelivered or replayed event is harmless.
func (r *Repository) UpsertProject(ctx context.Context, projectID, teamID uuid.UUID) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		INSERT INTO project_access (project_id, team_id)
		VALUES ($1, $2)
		ON CONFLICT (project_id) DO UPDATE SET team_id = EXCLUDED.team_id`, projectID, teamID)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

func (r *Repository) UpsertTeamMember(ctx context.Context, teamID, userID uuid.UUID, role string) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		INSERT INTO team_access (team_id, user_id, role)
		VALUES ($1, $2, $3)
		ON CONFLICT (team_id, user_id) DO UPDATE SET role = EXCLUDED.role`, teamID, userID, role)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

// FindProjectTeamForMember answers the Team of a Project the caller belongs
// to, in one lookup so a foreign Project and an unknown one are the same 404.
func (r *Repository) FindProjectTeamForMember(ctx context.Context, projectID, userID uuid.UUID) (uuid.UUID, error) {
	var teamID uuid.UUID
	err := r.queries.Executor(ctx).QueryRow(ctx, `
		SELECT p.team_id
		FROM project_access p
		JOIN team_access t ON t.team_id = p.team_id
		WHERE p.project_id = $1 AND t.user_id = $2`, projectID, userID).Scan(&teamID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrorProjectNotFound
	}
	if err != nil {
		return uuid.Nil, err
	}

	return teamID, nil
}

func (r *Repository) IsTeamMember(ctx context.Context, teamID, userID uuid.UUID) (bool, error) {
	var exists bool
	err := r.queries.Executor(ctx).QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM team_access WHERE team_id = $1 AND user_id = $2)`, teamID, userID).Scan(&exists)

	return exists, err
}
