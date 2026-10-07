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

// StartMail fills a new interval or one whose stop arrived first, and leaves
// a started one alone, so a redelivered or replayed start never moves the
// price the Package was chosen at. The stop already heard of stays.
func (r *Repository) StartMail(ctx context.Context, i *MailInterval) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		INSERT INTO mail_package_intervals (project_id, started_at, team_id, project_name, package_code, monthly_emails, monthly_price)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (project_id, started_at) DO UPDATE SET
			team_id = EXCLUDED.team_id, project_name = EXCLUDED.project_name, package_code = EXCLUDED.package_code,
			monthly_emails = EXCLUDED.monthly_emails, monthly_price = EXCLUDED.monthly_price, updated_at = now()
		WHERE mail_package_intervals.monthly_price IS NULL`,
		i.ProjectID, i.StartedAt, i.TeamID, i.ProjectName, i.PackageCode, i.MonthlyEmails, i.MonthlyPrice)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

// StopMail keeps the earliest stop it has heard of; a stop whose start has
// not arrived yet is kept for it.
func (r *Repository) StopMail(ctx context.Context, projectID, teamID uuid.UUID, startedAt, stoppedAt time.Time) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		INSERT INTO mail_package_intervals (project_id, started_at, team_id, stopped_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (project_id, started_at) DO UPDATE SET
			stopped_at = LEAST(mail_package_intervals.stopped_at, EXCLUDED.stopped_at), updated_at = now()`,
		projectID, startedAt, teamID, stoppedAt)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

// FindMailForTeam answers the started intervals only: one still waiting for
// its start has no price to charge.
func (r *Repository) FindMailForTeam(ctx context.Context, teamID uuid.UUID, from, to time.Time) ([]*MailInterval, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT project_id, team_id, project_name, package_code, monthly_emails, monthly_price, started_at, stopped_at
		FROM mail_package_intervals
		WHERE team_id = $1 AND monthly_price IS NOT NULL AND started_at < $3 AND (stopped_at IS NULL OR stopped_at > $2)
		ORDER BY started_at, project_id`, teamID, from, to)
	if err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}
	defer rows.Close()

	out := make([]*MailInterval, 0)
	for rows.Next() {
		var i MailInterval
		if err := rows.Scan(&i.ProjectID, &i.TeamID, &i.ProjectName, &i.PackageCode, &i.MonthlyEmails, &i.MonthlyPrice,
			&i.StartedAt, &i.StoppedAt); err != nil {
			return nil, ErrorCannotRead.Copy().SetError(err)
		}
		out = append(out, &i)
	}
	if err := rows.Err(); err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}

	return out, nil
}

// StartStorage fills a new interval or one whose stop arrived first, and
// leaves a started one alone, so a redelivered or replayed start never moves
// the price the Package was chosen at. The stop already heard of stays.
func (r *Repository) StartStorage(ctx context.Context, i *StorageInterval) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		INSERT INTO storage_package_intervals (storage_id, started_at, team_id, project_id, project_name, bucket_name, region,
			package_code, size_bytes, monthly_price)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (storage_id, started_at) DO UPDATE SET
			team_id = EXCLUDED.team_id, project_id = EXCLUDED.project_id, project_name = EXCLUDED.project_name,
			bucket_name = EXCLUDED.bucket_name, region = EXCLUDED.region, package_code = EXCLUDED.package_code,
			size_bytes = EXCLUDED.size_bytes, monthly_price = EXCLUDED.monthly_price, updated_at = now()
		WHERE storage_package_intervals.monthly_price IS NULL`,
		i.StorageID, i.StartedAt, i.TeamID, i.ProjectID, i.ProjectName, i.BucketName, i.Region, i.PackageCode, i.SizeBytes, i.MonthlyPrice)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

// StopStorage keeps the earliest stop it has heard of; a stop whose start
// has not arrived yet is kept for it.
func (r *Repository) StopStorage(ctx context.Context, storageID, teamID uuid.UUID, startedAt, stoppedAt time.Time) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		INSERT INTO storage_package_intervals (storage_id, started_at, team_id, stopped_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (storage_id, started_at) DO UPDATE SET
			stopped_at = LEAST(storage_package_intervals.stopped_at, EXCLUDED.stopped_at), updated_at = now()`,
		storageID, startedAt, teamID, stoppedAt)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

// FindStorageForTeam answers the started intervals only: one still waiting
// for its start has no price to charge.
func (r *Repository) FindStorageForTeam(ctx context.Context, teamID uuid.UUID, from, to time.Time) ([]*StorageInterval, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT storage_id, team_id, project_id, project_name, bucket_name, region, package_code, size_bytes, monthly_price,
			started_at, stopped_at
		FROM storage_package_intervals
		WHERE team_id = $1 AND monthly_price IS NOT NULL AND started_at < $3 AND (stopped_at IS NULL OR stopped_at > $2)
		ORDER BY started_at, storage_id`, teamID, from, to)
	if err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}
	defer rows.Close()

	out := make([]*StorageInterval, 0)
	for rows.Next() {
		var i StorageInterval
		if err := rows.Scan(&i.StorageID, &i.TeamID, &i.ProjectID, &i.ProjectName, &i.BucketName, &i.Region, &i.PackageCode,
			&i.SizeBytes, &i.MonthlyPrice, &i.StartedAt, &i.StoppedAt); err != nil {
			return nil, ErrorCannotRead.Copy().SetError(err)
		}
		out = append(out, &i)
	}
	if err := rows.Err(); err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}

	return out, nil
}

// TeamsWithUsage answers the Teams with a Droplet that ran or a Mail or
// Storage Package held at some point in [from, to).
func (r *Repository) TeamsWithUsage(ctx context.Context, from, to time.Time) ([]uuid.UUID, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT team_id FROM usage
		WHERE started_at IS NOT NULL AND started_at < $2 AND (stopped_at IS NULL OR stopped_at > $1)
		UNION
		SELECT team_id FROM mail_package_intervals
		WHERE monthly_price IS NOT NULL AND started_at < $2 AND (stopped_at IS NULL OR stopped_at > $1)
		UNION
		SELECT team_id FROM storage_package_intervals
		WHERE monthly_price IS NOT NULL AND started_at < $2 AND (stopped_at IS NULL OR stopped_at > $1)
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
