package project

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/piggystats/nodera/internal/persistence/postgresql"
)

const nameUniqueConstraint = "projects_team_name_unique"

type Repository struct {
	queries *postgresql.Queries
}

func NewRepository(queries *postgresql.Queries) *Repository {
	return &Repository{queries: queries}
}

func (r *Repository) Create(ctx context.Context, project *Project) error {
	if project.ID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return ErrorCannotCreate.Copy().SetError(err)
		}
		project.ID = id
	}

	err := r.queries.Executor(ctx).QueryRow(ctx, `
		INSERT INTO projects (id, team_id, name, description)
		VALUES ($1, $2, $3, $4)
		RETURNING created_at, updated_at`,
		project.ID, project.TeamID, project.Name, project.Description,
	).Scan(&project.CreatedAt, &project.UpdatedAt)
	if err != nil {
		if constraint, ok := postgresql.UniqueViolationConstraint(err); ok && constraint == nameUniqueConstraint {
			return ErrorNameTaken
		}

		return ErrorCannotCreate.Copy().SetError(err)
	}

	return nil
}

func (r *Repository) FindByTeamID(ctx context.Context, teamID uuid.UUID) (Projects, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT `+projectColumns+`
		FROM projects
		WHERE team_id = $1 AND deleted_at IS NULL
		ORDER BY created_at`, teamID)
	if err != nil {
		return nil, err
	}

	return scanProjects(rows)
}

// FindInTeam answers a live Project only through its own Team, so an id
// learned elsewhere reads as not found.
func (r *Repository) FindInTeam(ctx context.Context, teamID, projectID uuid.UUID) (*Project, error) {
	p, err := scanProject(r.queries.Executor(ctx).QueryRow(ctx, `
		SELECT `+projectColumns+`
		FROM projects
		WHERE id = $1 AND team_id = $2 AND deleted_at IS NULL`, projectID, teamID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrorNotFound
	}

	return p, err
}

// Lock holds a live Project of the Team for the rest of the transaction, so
// changes that count what the Project holds run one at a time.
func (r *Repository) Lock(ctx context.Context, teamID, projectID uuid.UUID) error {
	var id uuid.UUID
	err := r.queries.Executor(ctx).QueryRow(ctx, `
		SELECT id FROM projects
		WHERE id = $1 AND team_id = $2 AND deleted_at IS NULL
		FOR UPDATE`, projectID, teamID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrorNotFound
	}

	return err
}

// FindAll feeds the replay of domain events; it walks every live row on purpose.
func (r *Repository) FindAll(ctx context.Context) (Projects, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT `+projectColumns+`
		FROM projects
		WHERE deleted_at IS NULL
		ORDER BY created_at`)
	if err != nil {
		return nil, err
	}

	return scanProjects(rows)
}

const projectColumns = `id, team_id, name, description, created_at, updated_at`

func scanProjects(rows pgx.Rows) (Projects, error) {
	defer rows.Close()

	projects := make(Projects, 0)
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}

	return projects, rows.Err()
}

func scanProject(row pgx.Row) (*Project, error) {
	var p Project
	if err := row.Scan(&p.ID, &p.TeamID, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}

	return &p, nil
}
