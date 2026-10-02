package payment

import (
	"context"
	"errors"
	"time"

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

// StoreWebhookEvent inserts the event once and reports whether this call was
// the one that stored it; a replay answers false and changes nothing.
func (r *Repository) StoreWebhookEvent(ctx context.Context, e *WebhookEvent) (bool, error) {
	err := r.queries.Executor(ctx).QueryRow(ctx, `
		INSERT INTO webhook_events (provider, provider_event_id, kind, provider_payment_ref, status, body)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (provider, provider_event_id) DO NOTHING
		RETURNING id, received_at`,
		e.Provider, e.ProviderEventID, e.Kind, e.ProviderPaymentRef, e.Status, e.Body).Scan(&e.ID, &e.ReceivedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, ErrorCannotStore.Copy().SetError(err)
	}

	return true, nil
}

// FindUnprocessedWebhookEvents answers the events reconciliation has not
// handled yet, oldest first.
func (r *Repository) FindUnprocessedWebhookEvents(ctx context.Context, limit int) ([]*WebhookEvent, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT id, provider, provider_event_id, kind, provider_payment_ref, status, body, received_at, processed_at
		FROM webhook_events WHERE processed_at IS NULL ORDER BY received_at LIMIT $1`, limit)
	if err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}
	defer rows.Close()

	out := make([]*WebhookEvent, 0)
	for rows.Next() {
		var e WebhookEvent
		if err := rows.Scan(&e.ID, &e.Provider, &e.ProviderEventID, &e.Kind, &e.ProviderPaymentRef, &e.Status, &e.Body, &e.ReceivedAt, &e.ProcessedAt); err != nil {
			return nil, ErrorCannotRead.Copy().SetError(err)
		}
		out = append(out, &e)
	}
	if err := rows.Err(); err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}

	return out, nil
}

func (r *Repository) MarkWebhookEventProcessed(ctx context.Context, e *WebhookEvent) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `UPDATE webhook_events SET processed_at = now() WHERE id = $1`, e.ID)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Payments
// ---------------------------------------------------------------------------

const paymentColumns = `id, invoice_id, team_id, payment_method_id, provider, attempt, idempotency_key, trigger, amount, currency,
	status, provider_ref, failure_reason, confirmation_url, confirm_expires_at, created_at, updated_at, completed_at`

func scanPayment(row pgx.Row) (*Payment, error) {
	var p Payment
	err := row.Scan(&p.ID, &p.InvoiceID, &p.TeamID, &p.PaymentMethodID, &p.Provider, &p.Attempt, &p.IdempotencyKey, &p.Trigger,
		&p.Amount, &p.Currency, &p.Status, &p.ProviderRef, &p.FailureReason, &p.ConfirmationURL, &p.ConfirmExpiresAt,
		&p.CreatedAt, &p.UpdatedAt, &p.CompletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrorNotFound
	}
	if err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}

	return &p, nil
}

func scanPayments(rows pgx.Rows, err error) ([]*Payment, error) {
	if err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}
	defer rows.Close()

	out := make([]*Payment, 0)
	for rows.Next() {
		p, err := scanPayment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}

	return out, nil
}

// NextAttempt counts from the Invoice's highest attempt; the caller holds
// the Team's status row, so two attempts never take the same number.
func (r *Repository) NextAttempt(ctx context.Context, invoiceID uuid.UUID) (int, error) {
	var next int
	err := r.queries.Executor(ctx).QueryRow(ctx,
		`SELECT COALESCE(max(attempt), 0) + 1 FROM payments WHERE invoice_id = $1`, invoiceID).Scan(&next)
	if err != nil {
		return 0, ErrorCannotRead.Copy().SetError(err)
	}

	return next, nil
}

func (r *Repository) Create(ctx context.Context, p *Payment) error {
	err := r.queries.Executor(ctx).QueryRow(ctx, `
		INSERT INTO payments (id, invoice_id, team_id, payment_method_id, provider, attempt, idempotency_key, trigger, amount,
			currency, status, provider_ref, failure_reason, confirmation_url, confirm_expires_at, created_at, completed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		RETURNING updated_at`,
		p.ID, p.InvoiceID, p.TeamID, p.PaymentMethodID, p.Provider, p.Attempt, p.IdempotencyKey, p.Trigger, p.Amount,
		p.Currency, p.Status, p.ProviderRef, p.FailureReason, p.ConfirmationURL, p.ConfirmExpiresAt, p.CreatedAt, p.CompletedAt).
		Scan(&p.UpdatedAt)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

func (r *Repository) Save(ctx context.Context, p *Payment) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		UPDATE payments
		SET status = $2, provider_ref = $3, failure_reason = $4, confirmation_url = $5, confirm_expires_at = $6,
			completed_at = $7, updated_at = now()
		WHERE id = $1`, p.ID, p.Status, p.ProviderRef, p.FailureReason, p.ConfirmationURL, p.ConfirmExpiresAt, p.CompletedAt)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

func (r *Repository) FindByID(ctx context.Context, id uuid.UUID, forUpdate bool) (*Payment, error) {
	query := `SELECT ` + paymentColumns + ` FROM payments WHERE id = $1`
	if forUpdate {
		query += ` FOR UPDATE`
	}

	return scanPayment(r.queries.Executor(ctx).QueryRow(ctx, query, id))
}

func (r *Repository) FindByInvoiceID(ctx context.Context, invoiceID uuid.UUID) ([]*Payment, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx,
		`SELECT `+paymentColumns+` FROM payments WHERE invoice_id = $1 ORDER BY attempt`, invoiceID)

	return scanPayments(rows, err)
}

func (r *Repository) FindByProviderRef(ctx context.Context, provider, ref string) (*Payment, error) {
	return scanPayment(r.queries.Executor(ctx).QueryRow(ctx,
		`SELECT `+paymentColumns+` FROM payments WHERE provider = $1 AND provider_ref = $2 ORDER BY created_at DESC LIMIT 1`, provider, ref))
}

func (r *Repository) FindByIdempotencyKey(ctx context.Context, key string) (*Payment, error) {
	return scanPayment(r.queries.Executor(ctx).QueryRow(ctx,
		`SELECT `+paymentColumns+` FROM payments WHERE idempotency_key = $1`, key))
}

func (r *Repository) FindPending(ctx context.Context, before time.Time, limit int) ([]*Payment, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx,
		`SELECT `+paymentColumns+` FROM payments WHERE status = $1 AND created_at < $2 ORDER BY created_at LIMIT $3`, StatusPending, before, limit)

	return scanPayments(rows, err)
}

func (r *Repository) FindExpiredConfirmations(ctx context.Context, at time.Time, limit int) ([]*Payment, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx,
		`SELECT `+paymentColumns+` FROM payments WHERE status = $1 AND confirm_expires_at <= $2 ORDER BY confirm_expires_at LIMIT $3`,
		StatusConfirmationRequired, at, limit)

	return scanPayments(rows, err)
}
