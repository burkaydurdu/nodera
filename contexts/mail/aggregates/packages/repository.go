package packages

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/piggystats/nodera/internal/persistence/postgresql"
)

type Repository struct {
	queries *postgresql.Queries
}

func NewRepository(queries *postgresql.Queries) *Repository {
	return &Repository{queries: queries}
}

func (r *Repository) ListCatalog(ctx context.Context) ([]*Package, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT code, monthly_emails, monthly_price, active FROM packages WHERE active ORDER BY monthly_emails`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*Package{}
	for rows.Next() {
		var p Package
		if err := rows.Scan(&p.Code, &p.MonthlyEmails, &p.MonthlyPrice, &p.Active); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}

	return out, rows.Err()
}

// FindCatalog answers only a Package that is still sold.
func (r *Repository) FindCatalog(ctx context.Context, code string) (*Package, error) {
	p := Package{Code: code}
	err := r.queries.Executor(ctx).QueryRow(ctx, `
		SELECT monthly_emails, monthly_price, active FROM packages WHERE code = $1 AND active`, code).
		Scan(&p.MonthlyEmails, &p.MonthlyPrice, &p.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrorUnknownPackage
	}
	if err != nil {
		return nil, err
	}

	return &p, nil
}

const projectColumns = `project_id, team_id, code, monthly_emails, monthly_price, started_at,
	scheduled_code, scheduled_monthly_emails, scheduled_monthly_price`

// FindProject answers nil, without an error, for a Project that never had a
// Package.
func (r *Repository) FindProject(ctx context.Context, projectID uuid.UUID) (*ProjectPackage, error) {
	return r.findProject(ctx, `SELECT `+projectColumns+` FROM project_packages WHERE project_id = $1`, projectID)
}

func (r *Repository) FindProjectForUpdate(ctx context.Context, projectID uuid.UUID) (*ProjectPackage, error) {
	return r.findProject(ctx, `SELECT `+projectColumns+` FROM project_packages WHERE project_id = $1 FOR UPDATE`, projectID)
}

func (r *Repository) findProject(ctx context.Context, query string, projectID uuid.UUID) (*ProjectPackage, error) {
	pp, err := scanProject(r.queries.Executor(ctx).QueryRow(ctx, query, projectID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}

	return pp, err
}

func (r *Repository) SaveProject(ctx context.Context, pp *ProjectPackage) error {
	var code *string
	var emails *int
	var price *decimal.Decimal
	var started *time.Time
	if pp.Current != nil {
		code, emails, price, started = &pp.Current.Code, &pp.Current.MonthlyEmails, &pp.Current.MonthlyPrice, &pp.Current.StartedAt
	}
	var scheduledCode *string
	var scheduledEmails *int
	var scheduledPrice *decimal.Decimal
	if pp.Scheduled != nil {
		scheduledCode = &pp.Scheduled.Code
		if !pp.Scheduled.Cancels() {
			scheduledEmails, scheduledPrice = &pp.Scheduled.MonthlyEmails, &pp.Scheduled.MonthlyPrice
		}
	}

	_, err := r.queries.Executor(ctx).Exec(ctx, `
		INSERT INTO project_packages (project_id, team_id, code, monthly_emails, monthly_price, started_at,
			scheduled_code, scheduled_monthly_emails, scheduled_monthly_price, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now())
		ON CONFLICT (project_id) DO UPDATE
		SET code = EXCLUDED.code, monthly_emails = EXCLUDED.monthly_emails, monthly_price = EXCLUDED.monthly_price,
			started_at = EXCLUDED.started_at, scheduled_code = EXCLUDED.scheduled_code,
			scheduled_monthly_emails = EXCLUDED.scheduled_monthly_emails, scheduled_monthly_price = EXCLUDED.scheduled_monthly_price,
			updated_at = now()`,
		pp.ProjectID, pp.TeamID, code, emails, price, started, scheduledCode, scheduledEmails, scheduledPrice)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

func (r *Repository) ListHeldByTeam(ctx context.Context, teamID uuid.UUID) ([]*ProjectPackage, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT `+projectColumns+` FROM project_packages WHERE team_id = $1 AND code IS NOT NULL ORDER BY project_id`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*ProjectPackage
	for rows.Next() {
		pp, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, pp)
	}

	return out, rows.Err()
}

// ListRolloverDue is every Project still to be carried into the Period
// starting at periodStart: one that holds a Package, or has a change
// scheduled, and has no row for that Period yet.
func (r *Repository) ListRolloverDue(ctx context.Context, periodStart time.Time, limit int) ([]uuid.UUID, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT pp.project_id FROM project_packages pp
		WHERE (pp.code IS NOT NULL OR pp.scheduled_code IS NOT NULL)
		  AND (pp.started_at IS NULL OR pp.started_at < $1)
		  AND NOT EXISTS (SELECT 1 FROM mail_periods mp WHERE mp.project_id = pp.project_id AND mp.period_start = $1)
		ORDER BY pp.project_id LIMIT $2`, periodStart, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}

	return out, rows.Err()
}

// FindPeriod answers nil, without an error, for a Period never opened.
func (r *Repository) FindPeriod(ctx context.Context, projectID uuid.UUID, start time.Time) (*Period, error) {
	p := Period{ProjectID: projectID, Start: start}
	err := r.queries.Executor(ctx).QueryRow(ctx, `
		SELECT allowance, used, near_limit_notified_at, exhausted_notified_at
		FROM mail_periods WHERE project_id = $1 AND period_start = $2`, projectID, start).
		Scan(&p.Allowance, &p.Used, &p.NearLimitNotifiedAt, &p.ExhaustedNotifiedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &p, nil
}

// UpsertPeriod opens a Period or changes its allowance. The used count
// carries over; a threshold mark is cleared once the new allowance puts usage
// back under it, so a larger Package is warned about again.
func (r *Repository) UpsertPeriod(ctx context.Context, period *Period) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		INSERT INTO mail_periods (project_id, period_start, allowance)
		VALUES ($1, $2, $3)
		ON CONFLICT (project_id, period_start) DO UPDATE
		SET allowance = EXCLUDED.allowance,
			near_limit_notified_at = CASE WHEN mail_periods.used * 100 < EXCLUDED.allowance * $4 THEN NULL ELSE mail_periods.near_limit_notified_at END,
			exhausted_notified_at = CASE WHEN mail_periods.used < EXCLUDED.allowance THEN NULL ELSE mail_periods.exhausted_notified_at END`,
		period.ProjectID, period.Start, period.Allowance, NearLimitPercent)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

// AddUsage counts n emails only if they fit the allowance, in one statement
// so two concurrent sends can never both take its last emails.
func (r *Repository) AddUsage(ctx context.Context, projectID uuid.UUID, start time.Time, n int) (int, int, bool, error) {
	var after int
	err := r.queries.Executor(ctx).QueryRow(ctx, `
		UPDATE mail_periods SET used = used + $3
		WHERE project_id = $1 AND period_start = $2 AND used + $3 <= allowance
		RETURNING used`, projectID, start, n).Scan(&after)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, false, nil
	}
	if err != nil {
		return 0, 0, false, ErrorCannotStore.Copy().SetError(err)
	}

	return after - n, after, true, nil
}

func (r *Repository) MarkNearLimitNotified(ctx context.Context, projectID uuid.UUID, start, at time.Time) (bool, error) {
	return r.mark(ctx, `UPDATE mail_periods SET near_limit_notified_at = $3
		WHERE project_id = $1 AND period_start = $2 AND near_limit_notified_at IS NULL`, projectID, start, at)
}

func (r *Repository) MarkExhaustedNotified(ctx context.Context, projectID uuid.UUID, start, at time.Time) (bool, error) {
	return r.mark(ctx, `UPDATE mail_periods SET exhausted_notified_at = $3
		WHERE project_id = $1 AND period_start = $2 AND exhausted_notified_at IS NULL`, projectID, start, at)
}

// mark answers whether this call set the mark, so a threshold is announced by
// exactly one of two concurrent sends.
func (r *Repository) mark(ctx context.Context, query string, projectID uuid.UUID, start, at time.Time) (bool, error) {
	tag, err := r.queries.Executor(ctx).Exec(ctx, query, projectID, start, at)
	if err != nil {
		return false, ErrorCannotStore.Copy().SetError(err)
	}

	return tag.RowsAffected() > 0, nil
}

func scanProject(row pgx.Row) (*ProjectPackage, error) {
	var pp ProjectPackage
	var code, scheduledCode *string
	var emails, scheduledEmails *int
	var price, scheduledPrice *decimal.Decimal
	var started *time.Time
	if err := row.Scan(&pp.ProjectID, &pp.TeamID, &code, &emails, &price, &started, &scheduledCode, &scheduledEmails, &scheduledPrice); err != nil {
		return nil, err
	}
	if code != nil && emails != nil && price != nil && started != nil {
		pp.Current = &HeldPackage{Code: *code, MonthlyEmails: *emails, MonthlyPrice: *price, StartedAt: *started}
	}
	if scheduledCode != nil {
		pp.Scheduled = &Scheduled{Code: *scheduledCode}
		if scheduledEmails != nil && scheduledPrice != nil {
			pp.Scheduled.MonthlyEmails, pp.Scheduled.MonthlyPrice = *scheduledEmails, *scheduledPrice
		}
	}

	return &pp, nil
}
