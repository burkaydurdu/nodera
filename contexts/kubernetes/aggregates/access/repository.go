package access

import (
	"context"
	"errors"
	"time"

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

// UpsertProject is idempotent so a redelivered or replayed event is harmless.
// A replay of an event that predates names carries none and keeps the one
// already stored.
func (r *Repository) UpsertProject(ctx context.Context, project *ProjectAccess) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		INSERT INTO project_access (project_id, team_id, name)
		VALUES ($1, $2, $3)
		ON CONFLICT (project_id) DO UPDATE
		SET team_id = EXCLUDED.team_id,
		    name = CASE WHEN EXCLUDED.name = '' THEN project_access.name ELSE EXCLUDED.name END`,
		project.ProjectID, project.TeamID, project.Name)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

// The membership writes are last-writer-wins on the event's own time: the
// three events travel on separate queues, so a row only moves forward and an
// older event, redelivered or overtaken, changes nothing.

// UpsertTeamMember records a membership as of addedAt; being added again
// after a removal lifts the tombstone.
func (r *Repository) UpsertTeamMember(ctx context.Context, teamID, userID uuid.UUID, role authz.Role, addedAt time.Time) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		INSERT INTO team_access (team_id, user_id, role, updated_at, removed_at)
		VALUES ($1, $2, $3, $4, NULL)
		ON CONFLICT (team_id, user_id) DO UPDATE
		SET role = EXCLUDED.role, updated_at = EXCLUDED.updated_at, removed_at = NULL
		WHERE team_access.updated_at < EXCLUDED.updated_at`, teamID, userID, role, addedAt)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

// UpdateTeamMemberRole changes a membership this context already knows. A
// Role is only ever changed for a member, so a change newer than a removal
// means the member was invited again and that event is still on its way.
func (r *Repository) UpdateTeamMemberRole(ctx context.Context, teamID, userID uuid.UUID, role authz.Role, changedAt time.Time) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		UPDATE team_access
		SET role = $3, updated_at = $4, removed_at = NULL
		WHERE team_id = $1 AND user_id = $2 AND updated_at < $4`, teamID, userID, role, changedAt)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

// RemoveTeamMember tombstones the membership as of removedAt, writing the
// tombstone even for a member this context never saw so that their
// TeamMemberAdded, when it arrives late, cannot resurrect them.
func (r *Repository) RemoveTeamMember(ctx context.Context, teamID, userID uuid.UUID, removedAt time.Time) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		INSERT INTO team_access (team_id, user_id, role, updated_at, removed_at)
		VALUES ($1, $2, '', $3, $3)
		ON CONFLICT (team_id, user_id) DO UPDATE
		SET updated_at = EXCLUDED.updated_at, removed_at = EXCLUDED.removed_at
		WHERE team_access.updated_at < EXCLUDED.updated_at`, teamID, userID, removedAt)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

func (r *Repository) FindProject(ctx context.Context, projectID uuid.UUID) (*ProjectAccess, error) {
	p := ProjectAccess{ProjectID: projectID}
	err := r.queries.Executor(ctx).QueryRow(ctx, `
		SELECT team_id, name, created_at FROM project_access WHERE project_id = $1`, projectID).
		Scan(&p.TeamID, &p.Name, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrorProjectNotFound
	}
	if err != nil {
		return nil, err
	}

	return &p, nil
}

// FindProjectMembership answers the Team of a Project the caller belongs to
// and their Role in it, in one lookup so a foreign Project and an unknown one
// are the same 404.
func (r *Repository) FindProjectMembership(ctx context.Context, projectID, userID uuid.UUID) (*Membership, error) {
	var m Membership
	err := r.queries.Executor(ctx).QueryRow(ctx, `
		SELECT p.team_id, t.role
		FROM project_access p
		JOIN team_access t ON t.team_id = p.team_id
		WHERE p.project_id = $1 AND t.user_id = $2 AND t.removed_at IS NULL`, projectID, userID).Scan(&m.TeamID, &m.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrorProjectNotFound
	}
	if err != nil {
		return nil, err
	}

	return &m, nil
}
