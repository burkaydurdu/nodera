package droplet

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/piggystats/nodera/internal/persistence/postgresql"
)

const priceScale = 4

type Repository struct {
	queries *postgresql.Queries
}

func NewRepository(queries *postgresql.Queries) *Repository {
	return &Repository{queries: queries}
}

const columns = `
	id, project_id, team_id, plan_id, plan_code, region_code, image_code, hostname, hourly_price::text, monthly_cap_hours,
	status, provider_id, provider_ref, ipv4, ipv6, ssh_keys, pending_action, failure_reason, created_by,
	created_at, updated_at, active_at, deletion_requested_at, deleted_at`

// Create inserts the Droplet and its creating history row. It runs inside the
// caller's transaction so the row and its first history entry commit together.
func (r *Repository) Create(ctx context.Context, d *Droplet) error {
	if d.ID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return ErrorCannotCreate.Copy().SetError(err)
		}
		d.ID = id
	}

	keys, err := json.Marshal(d.SSHKeys)
	if err != nil {
		return ErrorCannotCreate.Copy().SetError(err)
	}

	err = r.queries.Executor(ctx).QueryRow(ctx, `
		INSERT INTO droplets (id, project_id, team_id, plan_id, plan_code, region_code, image_code, hostname, hourly_price,
		                      monthly_cap_hours, status, provider_id, ssh_keys, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::numeric, $10, $11, $12, $13, $14)
		RETURNING created_at, updated_at`,
		d.ID, d.ProjectID, d.TeamID, d.PlanID, d.PlanCode, d.RegionCode, d.ImageCode, d.Hostname, d.HourlyPrice.StringFixed(priceScale),
		d.MonthlyCapHours, string(d.Status), d.ProviderID, keys, d.CreatedBy,
	).Scan(&d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return ErrorCannotCreate.Copy().SetError(err)
	}

	return r.AddHistory(ctx, d.ID, &HistoryEntry{To: d.Status, At: d.CreatedAt, Reason: "created"})
}

func (r *Repository) FindByID(ctx context.Context, id uuid.UUID) (*Droplet, error) {
	d, err := scan(r.queries.Executor(ctx).QueryRow(ctx, `SELECT `+columns+` FROM droplets WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrorNotFound
	}

	return d, err
}

// FindByIDForUpdate locks the row for the transaction riding on ctx, so a
// read-modify-write cannot overwrite a change made in between; without a
// transaction the lock ends with the statement and it is a plain read.
func (r *Repository) FindByIDForUpdate(ctx context.Context, id uuid.UUID) (*Droplet, error) {
	d, err := scan(r.queries.Executor(ctx).QueryRow(ctx, `SELECT `+columns+` FROM droplets WHERE id = $1 FOR UPDATE`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrorNotFound
	}

	return d, err
}

// FindByProjectID lists what exists: deleted Droplets are hidden, newest first.
func (r *Repository) FindByProjectID(ctx context.Context, projectID uuid.UUID) (Droplets, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT `+columns+` FROM droplets
		WHERE project_id = $1 AND status <> 'deleted'
		ORDER BY created_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanAll(rows)
}

// FindTransitional answers the Droplets the poller must ask the Provider about.
func (r *Repository) FindTransitional(ctx context.Context) (Droplets, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT `+columns+` FROM droplets
		WHERE status IN ('provisioning', 'deleting') OR pending_action IS NOT NULL
		ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanAll(rows)
}

// Save persists every column a Droplet changes after creation.
func (r *Repository) Save(ctx context.Context, d *Droplet) error {
	var pending *string
	if d.PendingAction != nil {
		s := string(*d.PendingAction)
		pending = &s
	}

	_, err := r.queries.Executor(ctx).Exec(ctx, `
		UPDATE droplets
		SET status = $2, provider_ref = $3, ipv4 = $4, ipv6 = $5, pending_action = $6, failure_reason = $7,
		    active_at = $8, deletion_requested_at = $9, deleted_at = $10, updated_at = now()
		WHERE id = $1`,
		d.ID, string(d.Status), d.ProviderRef, d.IPv4, d.IPv6, pending, d.FailureReason,
		d.ActiveAt, d.DeletionRequest, d.DeletedAt)
	if err != nil {
		return ErrorCannotUpdate.Copy().SetError(err)
	}

	return nil
}

func (r *Repository) AddHistory(ctx context.Context, dropletID uuid.UUID, entry *HistoryEntry) error {
	var from *string
	if entry.From != nil {
		s := string(*entry.From)
		from = &s
	}

	_, err := r.queries.Executor(ctx).Exec(ctx, `
		INSERT INTO droplet_status_history (droplet_id, from_status, to_status, at, reason)
		VALUES ($1, $2, $3, $4, $5)`, dropletID, from, string(entry.To), entry.At, entry.Reason)
	if err != nil {
		return ErrorCannotUpdate.Copy().SetError(err)
	}

	return nil
}

func (r *Repository) ListHistory(ctx context.Context, dropletID uuid.UUID) (History, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT from_status, to_status, at, reason
		FROM droplet_status_history
		WHERE droplet_id = $1
		ORDER BY at, id`, dropletID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	history := make(History, 0)
	for rows.Next() {
		var (
			e    HistoryEntry
			from *string
			to   string
		)
		if err := rows.Scan(&from, &to, &e.At, &e.Reason); err != nil {
			return nil, err
		}
		if from != nil {
			s := Status(*from)
			e.From = &s
		}
		e.To = Status(to)
		history = append(history, &e)
	}

	return history, rows.Err()
}

func scanAll(rows pgx.Rows) (Droplets, error) {
	droplets := make(Droplets, 0)
	for rows.Next() {
		d, err := scan(rows)
		if err != nil {
			return nil, err
		}
		droplets = append(droplets, d)
	}

	return droplets, rows.Err()
}

func scan(row pgx.Row) (*Droplet, error) {
	var (
		d       Droplet
		price   string
		status  string
		keys    []byte
		pending *string
	)
	err := row.Scan(
		&d.ID, &d.ProjectID, &d.TeamID, &d.PlanID, &d.PlanCode, &d.RegionCode, &d.ImageCode, &d.Hostname, &price, &d.MonthlyCapHours,
		&status, &d.ProviderID, &d.ProviderRef, &d.IPv4, &d.IPv6, &keys, &pending, &d.FailureReason, &d.CreatedBy,
		&d.CreatedAt, &d.UpdatedAt, &d.ActiveAt, &d.DeletionRequest, &d.DeletedAt,
	)
	if err != nil {
		return nil, err
	}

	if d.HourlyPrice, err = decimal.NewFromString(price); err != nil {
		return nil, err
	}
	d.Status = Status(status)
	if pending != nil {
		a := Action(*pending)
		d.PendingAction = &a
	}
	d.SSHKeys = make([]SSHKey, 0)
	if len(keys) > 0 {
		if err := json.Unmarshal(keys, &d.SSHKeys); err != nil {
			return nil, err
		}
	}

	return &d, nil
}

// Now is the clock the repository stamps transitions with; tests may swap it.
var Now = time.Now
