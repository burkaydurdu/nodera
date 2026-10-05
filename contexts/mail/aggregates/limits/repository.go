package limits

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/piggystats/nodera/internal/persistence/postgresql"
)

const limitsColumns = `project_id, team_id, daily_limit, paused_at, pause_reason, rates_warned_at, rates_reset_at`

type Repository struct {
	queries *postgresql.Queries
}

func NewRepository(queries *postgresql.Queries) *Repository {
	return &Repository{queries: queries}
}

// Ensure gives a Project its starting Daily limit the first time it is seen
// and answers the row either way.
func (r *Repository) Ensure(ctx context.Context, projectID, teamID uuid.UUID) (*Limits, error) {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		INSERT INTO project_limits (project_id, team_id, daily_limit) VALUES ($1, $2, $3)
		ON CONFLICT (project_id) DO NOTHING`, projectID, teamID, InitialDailyLimit)
	if err != nil {
		return nil, ErrorCannotStore.Copy().SetError(err)
	}

	l, err := r.Find(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if l == nil {
		return nil, ErrorNotFound
	}

	return l, nil
}

// Find answers nil, without an error, for a Project that never sent.
func (r *Repository) Find(ctx context.Context, projectID uuid.UUID) (*Limits, error) {
	l, err := scanLimits(r.queries.Executor(ctx).QueryRow(ctx, `SELECT `+limitsColumns+` FROM project_limits WHERE project_id = $1`, projectID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}

	return l, err
}

// ConsumeDaily counts n emails against the day only if they fit its limit, in
// one statement so concurrent sends never pass it together. A limit raised
// during the day applies at once: the evaluation after midnight may run after
// the day's first sends.
func (r *Repository) ConsumeDaily(ctx context.Context, projectID uuid.UUID, day time.Time, limit, n int) (bool, error) {
	// A day's limit is never above the Project's, which only grows, so this
	// is the whole check for a day's first send.
	if n > limit {
		return false, nil
	}

	var sent int
	err := r.queries.Executor(ctx).QueryRow(ctx, `
		INSERT INTO mail_days (project_id, day, daily_limit, sent) VALUES ($1, $2, $3, $4)
		ON CONFLICT (project_id, day) DO UPDATE
		SET sent = mail_days.sent + EXCLUDED.sent, daily_limit = GREATEST(mail_days.daily_limit, EXCLUDED.daily_limit)
		WHERE mail_days.sent + EXCLUDED.sent <= GREATEST(mail_days.daily_limit, EXCLUDED.daily_limit)
		RETURNING sent`, projectID, day, limit, n).Scan(&sent)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, ErrorCannotStore.Copy().SetError(err)
	}

	return true, nil
}

// FindDay answers nil, without an error, for a day the Project did not send.
func (r *Repository) FindDay(ctx context.Context, projectID uuid.UUID, day time.Time) (*Day, error) {
	d := Day{ProjectID: projectID, Day: day}
	err := r.queries.Executor(ctx).QueryRow(ctx, `
		SELECT daily_limit, sent, delivered, bounced, complained, evaluated_at FROM mail_days
		WHERE project_id = $1 AND day = $2`, projectID, day).
		Scan(&d.Limit, &d.Sent, &d.Delivered, &d.Bounced, &d.Complained, &d.EvaluatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &d, nil
}

var outcomeColumns = map[Outcome]string{
	OutcomeDelivered:  "delivered",
	OutcomeBounced:    "bounced",
	OutcomeComplained: "complained",
}

// AddOutcome counts an outcome on the day it is recorded; a day with no send
// yet takes the Project's current limit as its own.
func (r *Repository) AddOutcome(ctx context.Context, projectID uuid.UUID, day time.Time, outcome Outcome, n int) error {
	column, ok := outcomeColumns[outcome]
	if !ok {
		return ErrorCannotStore.Copy().SetMessagef("unknown outcome %q", outcome)
	}

	query := fmt.Sprintf(`
		INSERT INTO mail_days (project_id, day, daily_limit, %[1]s)
		VALUES ($1, $2, COALESCE((SELECT daily_limit FROM project_limits WHERE project_id = $1), %[2]d), $3)
		ON CONFLICT (project_id, day) DO UPDATE SET %[1]s = mail_days.%[1]s + EXCLUDED.%[1]s`, column, InitialDailyLimit)
	if _, err := r.queries.Executor(ctx).Exec(ctx, query, projectID, day, n); err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

// Window sums the days from from to to, both included.
func (r *Repository) Window(ctx context.Context, projectID uuid.UUID, from, to time.Time) (Window, error) {
	var w Window
	err := r.queries.Executor(ctx).QueryRow(ctx, `
		SELECT COALESCE(sum(sent), 0), COALESCE(sum(delivered), 0), COALESCE(sum(bounced), 0), COALESCE(sum(complained), 0)
		FROM mail_days WHERE project_id = $1 AND day BETWEEN $2 AND $3`, projectID, from, to).
		Scan(&w.Sent, &w.Delivered, &w.Bounced, &w.Complained)

	return w, err
}

// ListActiveSince is every Project with a day recorded since from, the ones
// whose rates can have moved.
func (r *Repository) ListActiveSince(ctx context.Context, from time.Time) ([]*Limits, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT `+limitsColumns+` FROM project_limits l
		WHERE EXISTS (SELECT 1 FROM mail_days d WHERE d.project_id = l.project_id AND d.day >= $1)
		ORDER BY l.project_id`, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Limits
	for rows.Next() {
		l, err := scanLimits(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}

	return out, rows.Err()
}

// ListUnevaluated is every finished day whose limit decision is still owed.
func (r *Repository) ListUnevaluated(ctx context.Context, before time.Time, limit int) ([]*Day, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT project_id, day, daily_limit, sent, delivered, bounced, complained FROM mail_days
		WHERE day < $1 AND evaluated_at IS NULL ORDER BY day, project_id LIMIT $2`, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Day
	for rows.Next() {
		var d Day
		if err := rows.Scan(&d.ProjectID, &d.Day, &d.Limit, &d.Sent, &d.Delivered, &d.Bounced, &d.Complained); err != nil {
			return nil, err
		}
		out = append(out, &d)
	}

	return out, rows.Err()
}

// MarkEvaluated answers whether the day's decision was still owed, so each
// day is decided once however often the job runs.
func (r *Repository) MarkEvaluated(ctx context.Context, projectID uuid.UUID, day, at time.Time) (bool, error) {
	return r.affected(ctx, `UPDATE mail_days SET evaluated_at = $3
		WHERE project_id = $1 AND day = $2 AND evaluated_at IS NULL`, projectID, day, at)
}

// DoubleDailyLimit doubles the Project's limit as it stands rather than the
// day's: the day may have been sent before an earlier day's decision raised
// it.
func (r *Repository) DoubleDailyLimit(ctx context.Context, projectID uuid.UUID) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		UPDATE project_limits SET daily_limit = LEAST(daily_limit * 2, $2) WHERE project_id = $1`, projectID, MaxDailyLimit)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

// Pause answers whether this call paused the Project, so it is announced once.
func (r *Repository) Pause(ctx context.Context, projectID uuid.UUID, reason Reason, at time.Time) (bool, error) {
	return r.affected(ctx, `UPDATE project_limits SET paused_at = $3, pause_reason = $2
		WHERE project_id = $1 AND paused_at IS NULL`, projectID, reason, at)
}

func (r *Repository) Unpause(ctx context.Context, projectID uuid.UUID, at time.Time) (bool, error) {
	return r.affected(ctx, `UPDATE project_limits SET paused_at = NULL, pause_reason = NULL, rates_reset_at = $2
		WHERE project_id = $1 AND paused_at IS NOT NULL`, projectID, at)
}

// MarkRatesWarned answers whether a warning is due, at most one per WarnEvery.
func (r *Repository) MarkRatesWarned(ctx context.Context, projectID uuid.UUID, at time.Time) (bool, error) {
	return r.affected(ctx, `UPDATE project_limits SET rates_warned_at = $2
		WHERE project_id = $1 AND (rates_warned_at IS NULL OR rates_warned_at <= $3)`, projectID, at, at.Add(-WarnEvery))
}

func (r *Repository) affected(ctx context.Context, query string, args ...any) (bool, error) {
	tag, err := r.queries.Executor(ctx).Exec(ctx, query, args...)
	if err != nil {
		return false, ErrorCannotStore.Copy().SetError(err)
	}

	return tag.RowsAffected() > 0, nil
}

func scanLimits(row pgx.Row) (*Limits, error) {
	var l Limits
	var reason *string
	if err := row.Scan(&l.ProjectID, &l.TeamID, &l.DailyLimit, &l.PausedAt, &reason, &l.RatesWarnedAt, &l.RatesResetAt); err != nil {
		return nil, err
	}
	if reason != nil {
		r := Reason(*reason)
		l.PauseReason = &r
	}

	return &l, nil
}
