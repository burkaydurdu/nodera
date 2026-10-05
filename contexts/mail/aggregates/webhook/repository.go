package webhook

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/piggystats/nodera/contexts/mail/paging"
	"github.com/piggystats/nodera/internal/persistence/postgresql"
)

const (
	webhookColumns  = `id, project_id, url, events, active, secret, created_at, updated_at`
	deliveryColumns = `d.id, d.webhook_id, d.status, d.attempts, d.event_count, d.body, d.next_attempt_at, d.last_attempt_at,
		d.last_status_code, d.last_error, d.created_at`
)

type Repository struct {
	queries *postgresql.Queries
}

func NewRepository(queries *postgresql.Queries) *Repository {
	return &Repository{queries: queries}
}

func (r *Repository) Create(ctx context.Context, w *Webhook) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		INSERT INTO webhooks (id, project_id, url, events, active, secret, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, w.ID, w.ProjectID, w.URL, w.Events, w.Active, w.Secret, w.CreatedAt, w.UpdatedAt)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

func (r *Repository) Count(ctx context.Context, projectID uuid.UUID) (int, error) {
	var n int
	err := r.queries.Executor(ctx).QueryRow(ctx, `SELECT count(*) FROM webhooks WHERE project_id = $1`, projectID).Scan(&n)

	return n, err
}

func (r *Repository) List(ctx context.Context, projectID uuid.UUID) ([]*Webhook, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `SELECT `+webhookColumns+` FROM webhooks WHERE project_id = $1 ORDER BY created_at`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*Webhook{}
	for rows.Next() {
		w, err := scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}

	return out, rows.Err()
}

func (r *Repository) Find(ctx context.Context, projectID, id uuid.UUID) (*Webhook, error) {
	w, err := scanWebhook(r.queries.Executor(ctx).QueryRow(ctx, `SELECT `+webhookColumns+` FROM webhooks WHERE id = $1 AND project_id = $2`, id, projectID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrorNotFound
	}

	return w, err
}

func (r *Repository) Update(ctx context.Context, w *Webhook) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		UPDATE webhooks SET url = $2, events = $3, active = $4, updated_at = $5 WHERE id = $1`, w.ID, w.URL, w.Events, w.Active, w.UpdatedAt)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

func (r *Repository) Delete(ctx context.Context, projectID, id uuid.UUID) error {
	tag, err := r.queries.Executor(ctx).Exec(ctx, `DELETE FROM webhooks WHERE id = $1 AND project_id = $2`, id, projectID)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrorNotFound
	}

	return nil
}

// Enqueue queues an event for every active Webhook of the Project that wants
// its type, in the transaction that recorded the event.
func (r *Repository) Enqueue(ctx context.Context, projectID uuid.UUID, eventType string, payload []byte, at time.Time) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		INSERT INTO webhook_events (id, webhook_id, payload, created_at)
		SELECT gen_random_uuid(), w.id, $3, $4 FROM webhooks w
		WHERE w.project_id = $1 AND w.active AND $2 = ANY(w.events)`, projectID, eventType, payload, at)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

func (r *Repository) ListWithPending(ctx context.Context, limit int) ([]uuid.UUID, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT DISTINCT webhook_id FROM webhook_events WHERE delivery_id IS NULL LIMIT $1`, limit)
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

// Batch freezes up to limit pending events of a Webhook into one delivery
// and answers it, or nil when none was pending.
func (r *Repository) Batch(ctx context.Context, webhookID uuid.UUID, limit int, at time.Time) (*Delivery, error) {
	exec := r.queries.Executor(ctx)
	rows, err := exec.Query(ctx, `
		SELECT id, payload FROM webhook_events WHERE webhook_id = $1 AND delivery_id IS NULL
		ORDER BY seq LIMIT $2 FOR UPDATE SKIP LOCKED`, webhookID, limit)
	if err != nil {
		return nil, err
	}

	var ids []uuid.UUID
	var payloads [][]byte
	for rows.Next() {
		var id uuid.UUID
		var payload []byte
		if err := rows.Scan(&id, &payload); err != nil {
			rows.Close()

			return nil, err
		}
		ids = append(ids, id)
		payloads = append(payloads, payload)
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(ids) == 0 {
		return nil, err
	}

	body := append(append([]byte("["), bytes.Join(payloads, []byte(","))...), ']')
	next := at
	d := &Delivery{ID: uuid.New(), WebhookID: webhookID, Status: StatusPending, EventCount: len(ids), Body: body, NextAttemptAt: &next, CreatedAt: at}

	if _, err := exec.Exec(ctx, `
		INSERT INTO webhook_deliveries (id, webhook_id, status, event_count, body, next_attempt_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, d.ID, d.WebhookID, d.Status, d.EventCount, d.Body, d.NextAttemptAt, d.CreatedAt); err != nil {
		return nil, ErrorCannotStore.Copy().SetError(err)
	}
	if _, err := exec.Exec(ctx, `UPDATE webhook_events SET delivery_id = $1 WHERE id = ANY($2)`, d.ID, ids); err != nil {
		return nil, ErrorCannotStore.Copy().SetError(err)
	}

	return d, nil
}

// ListDue claims the deliveries whose next attempt has come.
func (r *Repository) ListDue(ctx context.Context, now time.Time, limit int) ([]*DueDelivery, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT `+deliveryColumns+`, w.url, w.secret, w.active
		FROM webhook_deliveries d JOIN webhooks w ON w.id = d.webhook_id
		WHERE d.status = 'pending' AND d.next_attempt_at <= $1
		ORDER BY d.next_attempt_at LIMIT $2
		FOR UPDATE OF d SKIP LOCKED`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*DueDelivery
	for rows.Next() {
		var d Delivery
		var due DueDelivery
		if err := rows.Scan(&d.ID, &d.WebhookID, &d.Status, &d.Attempts, &d.EventCount, &d.Body, &d.NextAttemptAt, &d.LastAttemptAt,
			&d.LastStatusCode, &d.LastError, &d.CreatedAt, &due.URL, &due.Secret, &due.Active); err != nil {
			return nil, err
		}
		due.Delivery = &d
		out = append(out, &due)
	}

	return out, rows.Err()
}

func (r *Repository) RecordAttempt(ctx context.Context, d *Delivery) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		UPDATE webhook_deliveries
		SET status = $2, attempts = $3, next_attempt_at = $4, last_attempt_at = $5, last_status_code = $6, last_error = $7
		WHERE id = $1`, d.ID, d.Status, d.Attempts, d.NextAttemptAt, d.LastAttemptAt, d.LastStatusCode, d.LastError)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

func (r *Repository) ListDeliveries(ctx context.Context, webhookID uuid.UUID, before *paging.Cursor, limit int) ([]*Delivery, *paging.Cursor, error) {
	query := `SELECT ` + deliveryColumns + ` FROM webhook_deliveries d WHERE d.webhook_id = $1`
	args := []any{webhookID}
	if before != nil {
		query += ` AND (d.created_at, d.id) < ($2, $3)`
		args = append(args, before.At, before.ID)
	}
	args = append(args, limit+1)
	query += ` ORDER BY d.created_at DESC, d.id DESC LIMIT $` + strconv.Itoa(len(args))

	rows, err := r.queries.Executor(ctx).Query(ctx, query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	out := []*Delivery{}
	for rows.Next() {
		var d Delivery
		if err := rows.Scan(&d.ID, &d.WebhookID, &d.Status, &d.Attempts, &d.EventCount, &d.Body, &d.NextAttemptAt, &d.LastAttemptAt,
			&d.LastStatusCode, &d.LastError, &d.CreatedAt); err != nil {
			return nil, nil, err
		}
		out = append(out, &d)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	var next *paging.Cursor
	if len(out) > limit {
		out = out[:limit]
		last := out[len(out)-1]
		next = &paging.Cursor{At: last.CreatedAt, ID: last.ID}
	}

	return out, next, nil
}

// ExpireBefore drops deliveries past retention and events that never left
// because their Webhook was disabled.
func (r *Repository) ExpireBefore(ctx context.Context, before time.Time) error {
	exec := r.queries.Executor(ctx)
	if _, err := exec.Exec(ctx, `DELETE FROM webhook_events WHERE created_at < $1`, before); err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}
	if _, err := exec.Exec(ctx, `DELETE FROM webhook_deliveries WHERE created_at < $1`, before); err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

func scanWebhook(row pgx.Row) (*Webhook, error) {
	var w Webhook
	if err := row.Scan(&w.ID, &w.ProjectID, &w.URL, &w.Events, &w.Active, &w.Secret, &w.CreatedAt, &w.UpdatedAt); err != nil {
		return nil, err
	}

	return &w, nil
}
