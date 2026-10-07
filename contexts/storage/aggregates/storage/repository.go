package storage

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/piggystats/nodera/internal/persistence/postgresql"
)

const bucketNameIndex = "storages_bucket_name_idx"

type Repository struct {
	queries *postgresql.Queries
}

func NewRepository(queries *postgresql.Queries) *Repository {
	return &Repository{queries: queries}
}

const columns = `id, project_id, team_id, region, bucket_name, status, package_code, size_bytes, monthly_price,
	package_started_at, scheduled_code, scheduled_size_bytes, scheduled_monthly_price, scheduled_at, paused_at, pause_reason,
	team_suspended, used_bytes, object_count, measured_at, nearly_full_notified_at, full_notified_at,
	engine_size_bytes, engine_stopped, traffic_read_until, created_by, created_at, deleted_at`

// shown are the Storages a member sees; one still opening or fully deleted is
// not there for anybody.
const shown = `status IN ('active', 'deleting')`

// Reserve takes the Bucket name in its Region for a Storage that is about to
// be opened. The unique index is what settles two requests for one name.
func (r *Repository) Reserve(ctx context.Context, s *Storage) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		INSERT INTO storages (id, project_id, team_id, region, bucket_name, status, package_code, size_bytes,
			monthly_price, created_by, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		s.ID, s.ProjectID, s.TeamID, s.Region, s.BucketName, string(StatusOpening), s.Package.Code, s.Package.SizeBytes,
		s.Package.MonthlyPrice, s.CreatedBy, s.CreatedAt)
	if constraint, ok := postgresql.UniqueViolationConstraint(err); ok && constraint == bucketNameIndex {
		return ErrorBucketNameTaken
	}
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

// Activate opens a reserved Storage: its Package starts and the engine holds
// its size limit.
func (r *Repository) Activate(ctx context.Context, id uuid.UUID, startedAt time.Time) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		UPDATE storages SET status = 'active', package_started_at = $2, engine_size_bytes = size_bytes, updated_at = now()
		WHERE id = $1 AND status = 'opening'`, id, startedAt)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

// Release gives a reservation back when the Storage was never opened.
func (r *Repository) Release(ctx context.Context, id uuid.UUID) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `DELETE FROM storages WHERE id = $1 AND status = 'opening'`, id)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

func (r *Repository) Find(ctx context.Context, projectID, id uuid.UUID) (*Storage, error) {
	return r.one(ctx, `SELECT `+columns+` FROM storages WHERE id = $1 AND project_id = $2 AND `+shown, id, projectID)
}

func (r *Repository) FindForUpdate(ctx context.Context, projectID, id uuid.UUID) (*Storage, error) {
	return r.one(ctx, `SELECT `+columns+` FROM storages WHERE id = $1 AND project_id = $2 AND `+shown+` FOR UPDATE`, id, projectID)
}

// FindByID is for the jobs, the consumers and the operator, who come with a
// Storage and no Project.
func (r *Repository) FindByID(ctx context.Context, id uuid.UUID) (*Storage, error) {
	return r.one(ctx, `SELECT `+columns+` FROM storages WHERE id = $1 AND `+shown, id)
}

func (r *Repository) FindByIDForUpdate(ctx context.Context, id uuid.UUID) (*Storage, error) {
	return r.one(ctx, `SELECT `+columns+` FROM storages WHERE id = $1 AND `+shown+` FOR UPDATE`, id)
}

func (r *Repository) one(ctx context.Context, query string, args ...any) (*Storage, error) {
	s, err := scan(r.queries.Executor(ctx).QueryRow(ctx, query, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrorNotFound
	}

	return s, err
}

func (r *Repository) ListByProject(ctx context.Context, projectID uuid.UUID) ([]*Storage, error) {
	return r.list(ctx, `SELECT `+columns+` FROM storages WHERE project_id = $1 AND `+shown+` ORDER BY created_at, id`, projectID)
}

// CountByProject counts every Storage still holding a Bucket name, so one
// being opened or deleted keeps its place.
func (r *Repository) CountByProject(ctx context.Context, projectID uuid.UUID) (int, error) {
	var n int
	err := r.queries.Executor(ctx).QueryRow(ctx, `
		SELECT count(*) FROM storages WHERE project_id = $1 AND status <> 'deleted'`, projectID).Scan(&n)

	return n, err
}

func (r *Repository) ListActiveByTeam(ctx context.Context, teamID uuid.UUID) ([]*Storage, error) {
	return r.list(ctx, `SELECT `+columns+` FROM storages WHERE team_id = $1 AND status = 'active' ORDER BY id`, teamID)
}

// ListActive pages every open Storage by id, for the jobs that visit them all.
func (r *Repository) ListActive(ctx context.Context, after uuid.UUID, limit int) ([]*Storage, error) {
	return r.list(ctx, `SELECT `+columns+` FROM storages WHERE status = 'active' AND id > $1 ORDER BY id LIMIT $2`, after, limit)
}

// ListScheduledDue is every Storage whose smaller Package is due by
// periodStart: scheduled before it.
func (r *Repository) ListScheduledDue(ctx context.Context, periodStart time.Time, limit int) ([]uuid.UUID, error) {
	return r.ids(ctx, `
		SELECT id FROM storages
		WHERE status = 'active' AND scheduled_code IS NOT NULL AND scheduled_at < $1
		ORDER BY id LIMIT $2`, periodStart, limit)
}

func (r *Repository) ListDeleting(ctx context.Context, limit int) ([]*Storage, error) {
	return r.list(ctx, `SELECT `+columns+` FROM storages WHERE status = 'deleting' ORDER BY deleted_at, id LIMIT $1`, limit)
}

// ListAbandoned is the reservations no request is finishing any more.
func (r *Repository) ListAbandoned(ctx context.Context, before time.Time, limit int) ([]*Storage, error) {
	return r.list(ctx, `SELECT `+columns+` FROM storages WHERE status = 'opening' AND created_at < $1 ORDER BY id LIMIT $2`, before, limit)
}

// ListOutOfSync is the open Storages whose engine does not hold the size
// limit or the stop the row says.
func (r *Repository) ListOutOfSync(ctx context.Context, limit int) ([]uuid.UUID, error) {
	return r.ids(ctx, `
		SELECT id FROM storages
		WHERE status = 'active'
		  AND (engine_size_bytes <> size_bytes OR engine_stopped <> (paused_at IS NOT NULL OR team_suspended))
		ORDER BY id LIMIT $1`, limit)
}

// Save writes everything about a Storage that changes after it is opened. The
// caller holds the row (FindForUpdate) so that two changes never overwrite
// each other.
func (r *Repository) Save(ctx context.Context, s *Storage) error {
	var scheduledCode *string
	var scheduledSize *int64
	var scheduledPrice *decimal.Decimal
	var scheduledAt *time.Time
	if s.Scheduled != nil {
		scheduledCode, scheduledSize, scheduledPrice, scheduledAt = &s.Scheduled.Code, &s.Scheduled.SizeBytes, &s.Scheduled.MonthlyPrice, &s.Scheduled.At
	}
	var pausedAt *time.Time
	var pauseReason *string
	if s.Pause != nil {
		reason := string(s.Pause.Reason)
		pausedAt, pauseReason = &s.Pause.At, &reason
	}

	_, err := r.queries.Executor(ctx).Exec(ctx, `
		UPDATE storages
		SET status = $2, package_code = $3, size_bytes = $4, monthly_price = $5, package_started_at = $6,
			scheduled_code = $7, scheduled_size_bytes = $8, scheduled_monthly_price = $9, scheduled_at = $10, paused_at = $11,
			pause_reason = $12, team_suspended = $13, used_bytes = $14, object_count = $15, measured_at = $16,
			nearly_full_notified_at = $17, full_notified_at = $18, engine_size_bytes = $19, engine_stopped = $20,
			traffic_read_until = $21, deleted_at = $22, updated_at = now()
		WHERE id = $1`,
		s.ID, string(s.Status), s.Package.Code, s.Package.SizeBytes, s.Package.MonthlyPrice, s.Package.StartedAt,
		scheduledCode, scheduledSize, scheduledPrice, scheduledAt, pausedAt, pauseReason, s.TeamSuspended, s.UsedBytes, s.ObjectCount,
		s.MeasuredAt, s.NearlyFullNotifiedAt, s.FullNotifiedAt, s.EngineSizeBytes, s.EngineStopped, s.TrafficReadUntil,
		s.DeletedAt)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

// SetTeamSuspended carries the Team's Billing status onto its Storages, one
// still opening included so that it is stopped as soon as it is active, and
// answers the open ones it changed.
func (r *Repository) SetTeamSuspended(ctx context.Context, teamID uuid.UUID, suspended bool) ([]uuid.UUID, error) {
	return r.ids(ctx, `
		UPDATE storages SET team_suspended = $2, updated_at = now()
		WHERE team_id = $1 AND status IN ('opening', 'active') AND team_suspended <> $2
		RETURNING CASE WHEN status = 'active' THEN id END`, teamID, suspended)
}

// MarkDeleted ends a deletion: the files are gone and the Bucket name is free.
func (r *Repository) MarkDeleted(ctx context.Context, id uuid.UUID, at time.Time) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		UPDATE storages SET status = 'deleted', purged_at = $2, updated_at = now() WHERE id = $1 AND status = 'deleting'`, id, at)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

// FindAccount answers nil, without an error, for a Project that never opened
// a Storage.
func (r *Repository) FindAccount(ctx context.Context, projectID uuid.UUID) (*Account, error) {
	a := Account{ProjectID: projectID}
	err := r.queries.Executor(ctx).QueryRow(ctx, `
		SELECT account_id, access_key, secret_key FROM project_accounts WHERE project_id = $1`, projectID).
		Scan(&a.AccountID, &a.AccessKey, &a.SealedSecret)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &a, nil
}

// SaveAccount keeps the first account stored for a Project: the engine
// answers the same one to two requests racing to open its first Storage.
func (r *Repository) SaveAccount(ctx context.Context, a *Account) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		INSERT INTO project_accounts (project_id, account_id, access_key, secret_key)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (project_id) DO NOTHING`, a.ProjectID, a.AccountID, a.AccessKey, a.SealedSecret)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

func (r *Repository) list(ctx context.Context, query string, args ...any) ([]*Storage, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*Storage{}
	for rows.Next() {
		s, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}

	return out, rows.Err()
}

func (r *Repository) ids(ctx context.Context, query string, args ...any) ([]uuid.UUID, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []uuid.UUID
	for rows.Next() {
		var id *uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if id != nil {
			out = append(out, *id)
		}
	}

	return out, rows.Err()
}

func scan(row pgx.Row) (*Storage, error) {
	var s Storage
	var status string
	var startedAt, scheduledAt, pausedAt *time.Time
	var scheduledCode, pauseReason *string
	var scheduledSize *int64
	var scheduledPrice *decimal.Decimal
	err := row.Scan(&s.ID, &s.ProjectID, &s.TeamID, &s.Region, &s.BucketName, &status, &s.Package.Code, &s.Package.SizeBytes,
		&s.Package.MonthlyPrice, &startedAt, &scheduledCode, &scheduledSize, &scheduledPrice, &scheduledAt, &pausedAt, &pauseReason,
		&s.TeamSuspended, &s.UsedBytes, &s.ObjectCount, &s.MeasuredAt, &s.NearlyFullNotifiedAt, &s.FullNotifiedAt,
		&s.EngineSizeBytes, &s.EngineStopped, &s.TrafficReadUntil, &s.CreatedBy, &s.CreatedAt, &s.DeletedAt)
	if err != nil {
		return nil, err
	}

	s.Status = Status(status)
	if startedAt != nil {
		s.Package.StartedAt = *startedAt
	}
	if scheduledCode != nil && scheduledSize != nil && scheduledPrice != nil && scheduledAt != nil {
		s.Scheduled = &Scheduled{Code: *scheduledCode, SizeBytes: *scheduledSize, MonthlyPrice: *scheduledPrice, At: *scheduledAt}
	}
	if pausedAt != nil && pauseReason != nil {
		s.Pause = &Pause{Reason: Reason(*pauseReason), At: *pausedAt}
	}

	return &s, nil
}
