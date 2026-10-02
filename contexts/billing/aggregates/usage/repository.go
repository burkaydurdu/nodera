package usage

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/piggystats/nodera/internal/persistence/postgresql"
)

type Repository struct {
	queries *postgresql.Queries
}

func NewRepository(queries *postgresql.Queries) *Repository {
	return &Repository{queries: queries}
}

// Start fills a new row or a tombstone and leaves a started row alone, so a
// redelivered or replayed start never moves the Locked price or the start.
// A tombstone keeps its stop when the stop came after the start: the
// Droplet is not brought back to life by its own late start event.
func (r *Repository) Start(ctx context.Context, u *Usage) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		INSERT INTO usage (droplet_id, team_id, project_id, hostname, plan_code, hourly_price, started_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (droplet_id) DO UPDATE SET
			team_id = EXCLUDED.team_id, project_id = EXCLUDED.project_id, hostname = EXCLUDED.hostname,
			plan_code = EXCLUDED.plan_code, hourly_price = EXCLUDED.hourly_price, started_at = EXCLUDED.started_at,
			stopped_at = CASE WHEN usage.stopped_at > EXCLUDED.started_at THEN usage.stopped_at ELSE NULL END,
			updated_at = now()
		WHERE usage.started_at IS NULL`,
		u.DropletID, u.TeamID, u.ProjectID, u.Hostname, u.PlanCode, u.HourlyPrice, u.StartedAt)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

// Stop keeps the earliest stop it has heard of; a stop without a start yet
// is stored as a tombstone.
func (r *Repository) Stop(ctx context.Context, dropletID, teamID uuid.UUID, stoppedAt time.Time) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		INSERT INTO usage (droplet_id, team_id, stopped_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (droplet_id) DO UPDATE SET
			stopped_at = LEAST(usage.stopped_at, EXCLUDED.stopped_at), updated_at = now()`,
		dropletID, teamID, stoppedAt)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

func (r *Repository) FindForTeam(ctx context.Context, teamID uuid.UUID, from, to time.Time) ([]*Usage, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT droplet_id, team_id, project_id, hostname, plan_code, hourly_price, started_at, stopped_at
		FROM usage
		WHERE team_id = $1 AND started_at IS NOT NULL AND started_at < $3 AND (stopped_at IS NULL OR stopped_at > $2)
		ORDER BY started_at, droplet_id`, teamID, from, to)
	if err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}
	defer rows.Close()

	out := make([]*Usage, 0)
	for rows.Next() {
		var (
			u         Usage
			projectID *uuid.UUID
			price     decimal.NullDecimal
		)
		if err := rows.Scan(&u.DropletID, &u.TeamID, &projectID, &u.Hostname, &u.PlanCode, &price, &u.StartedAt, &u.StoppedAt); err != nil {
			return nil, ErrorCannotRead.Copy().SetError(err)
		}
		if projectID != nil {
			u.ProjectID = *projectID
		}
		u.HourlyPrice = price.Decimal
		out = append(out, &u)
	}
	if err := rows.Err(); err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}

	return out, nil
}

func (r *Repository) TeamsWithUsage(ctx context.Context, from, to time.Time) ([]uuid.UUID, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT DISTINCT team_id FROM usage
		WHERE started_at IS NOT NULL AND started_at < $2 AND (stopped_at IS NULL OR stopped_at > $1)
		ORDER BY team_id`, from, to)
	if err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}
	defer rows.Close()

	out := make([]uuid.UUID, 0)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, ErrorCannotRead.Copy().SetError(err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}

	return out, nil
}

func (r *Repository) FirstStartedAt(ctx context.Context, teamID uuid.UUID) (*time.Time, error) {
	var first *time.Time
	if err := r.queries.Executor(ctx).QueryRow(ctx,
		`SELECT min(started_at) FROM usage WHERE team_id = $1`, teamID).Scan(&first); err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}

	return first, nil
}
