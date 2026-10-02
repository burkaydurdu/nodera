package paymentmethod

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

const methodColumns = `id, team_id, provider, currency, provider_customer_ref, provider_card_ref, brand, last4,
	exp_month, exp_year, is_default, created_by, created_at, deleted_at, expiry_notified_at`

func scanMethod(row pgx.Row) (*PaymentMethod, error) {
	var m PaymentMethod
	err := row.Scan(&m.ID, &m.TeamID, &m.Provider, &m.Currency, &m.ProviderCustomerRef, &m.ProviderCardRef, &m.Brand, &m.Last4,
		&m.ExpMonth, &m.ExpYear, &m.IsDefault, &m.CreatedBy, &m.CreatedAt, &m.DeletedAt, &m.ExpiryNotifiedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrorNotFound
	}
	if err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}

	return &m, nil
}

func (r *Repository) Create(ctx context.Context, m *PaymentMethod) error {
	err := r.queries.Executor(ctx).QueryRow(ctx, `
		INSERT INTO payment_methods (team_id, provider, currency, provider_customer_ref, provider_card_ref, brand, last4,
			exp_month, exp_year, is_default, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, created_at`,
		m.TeamID, m.Provider, m.Currency, m.ProviderCustomerRef, m.ProviderCardRef, m.Brand, m.Last4,
		m.ExpMonth, m.ExpYear, m.IsDefault, m.CreatedBy).Scan(&m.ID, &m.CreatedAt)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

// FindByID answers a live card of the Team; a deleted card or another Team's
// card does not exist for the caller.
func (r *Repository) FindByID(ctx context.Context, teamID, id uuid.UUID) (*PaymentMethod, error) {
	return scanMethod(r.queries.Executor(ctx).QueryRow(ctx,
		`SELECT `+methodColumns+` FROM payment_methods WHERE id = $1 AND team_id = $2 AND deleted_at IS NULL`, id, teamID))
}

// FindByTeamID answers the Team's live cards, default first, then oldest first.
func (r *Repository) FindByTeamID(ctx context.Context, teamID uuid.UUID) (PaymentMethods, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx,
		`SELECT `+methodColumns+` FROM payment_methods WHERE team_id = $1 AND deleted_at IS NULL ORDER BY is_default DESC, created_at, id`, teamID)
	if err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}
	defer rows.Close()

	methods := make(PaymentMethods, 0)
	for rows.Next() {
		m, err := scanMethod(rows)
		if err != nil {
			return nil, err
		}
		methods = append(methods, m)
	}
	if err := rows.Err(); err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}

	return methods, nil
}

// FindUnwarnedDefaults answers every live default card whose expiry warning
// has not gone out, those without a known expiry included; the caller
// decides which expire soon.
func (r *Repository) FindUnwarnedDefaults(ctx context.Context) (PaymentMethods, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx,
		`SELECT `+methodColumns+` FROM payment_methods
		WHERE is_default AND deleted_at IS NULL AND expiry_notified_at IS NULL
		ORDER BY exp_year, exp_month, id`)
	if err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}
	defer rows.Close()

	methods := make(PaymentMethods, 0)
	for rows.Next() {
		m, err := scanMethod(rows)
		if err != nil {
			return nil, err
		}
		methods = append(methods, m)
	}
	if err := rows.Err(); err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}

	return methods, nil
}

// UpdateExpiry stores an expiry the provider reported after the card was
// saved without one.
func (r *Repository) UpdateExpiry(ctx context.Context, id uuid.UUID, month, year int) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `UPDATE payment_methods SET exp_month = $2, exp_year = $3 WHERE id = $1`, id, month, year)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

// MarkExpiryNotified records the warning once; false when another run
// recorded it first.
func (r *Repository) MarkExpiryNotified(ctx context.Context, id uuid.UUID, at time.Time) (bool, error) {
	tag, err := r.queries.Executor(ctx).Exec(ctx,
		`UPDATE payment_methods SET expiry_notified_at = $2 WHERE id = $1 AND expiry_notified_at IS NULL`, id, at)
	if err != nil {
		return false, ErrorCannotStore.Copy().SetError(err)
	}

	return tag.RowsAffected() == 1, nil
}

// FindAnyByID answers a card of the Team even after its deletion: a Payment
// that charged it still names it.
func (r *Repository) FindAnyByID(ctx context.Context, teamID, id uuid.UUID) (*PaymentMethod, error) {
	return scanMethod(r.queries.Executor(ctx).QueryRow(ctx,
		`SELECT `+methodColumns+` FROM payment_methods WHERE id = $1 AND team_id = $2`, id, teamID))
}

// CountByTeamID counts the live cards; the country-change rule and the
// first/last-card status transitions hang on it.
func (r *Repository) CountByTeamID(ctx context.Context, teamID uuid.UUID) (int, error) {
	var n int
	err := r.queries.Executor(ctx).QueryRow(ctx,
		`SELECT count(*) FROM payment_methods WHERE team_id = $1 AND deleted_at IS NULL`, teamID).Scan(&n)
	if err != nil {
		return 0, ErrorCannotRead.Copy().SetError(err)
	}

	return n, nil
}

// SetDefault moves the default to one card in two statements inside the
// caller's transaction; the partial unique index guards the invariant.
func (r *Repository) SetDefault(ctx context.Context, teamID, id uuid.UUID) error {
	executor := r.queries.Executor(ctx)
	if _, err := executor.Exec(ctx,
		`UPDATE payment_methods SET is_default = false WHERE team_id = $1 AND deleted_at IS NULL AND is_default`, teamID); err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}
	tag, err := executor.Exec(ctx,
		`UPDATE payment_methods SET is_default = true WHERE id = $1 AND team_id = $2 AND deleted_at IS NULL`, id, teamID)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrorNotFound
	}

	return nil
}

// Delete is a soft delete: the row stays for the Payments that referenced it.
func (r *Repository) Delete(ctx context.Context, teamID, id uuid.UUID, at time.Time) error {
	tag, err := r.queries.Executor(ctx).Exec(ctx,
		`UPDATE payment_methods SET is_default = false, deleted_at = $3 WHERE id = $1 AND team_id = $2 AND deleted_at IS NULL`, id, teamID, at)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrorNotFound
	}

	return nil
}

// ---------------------------------------------------------------------------
// Verifications
// ---------------------------------------------------------------------------

const verificationColumns = `id, team_id, provider, currency, provider_ref, status, payment_method_id, failure_reason,
	created_by, created_at, completed_at`

func (r *Repository) CreateVerification(ctx context.Context, v *Verification) error {
	err := r.queries.Executor(ctx).QueryRow(ctx, `
		INSERT INTO card_verifications (id, team_id, provider, currency, provider_ref, status, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at`,
		v.ID, v.TeamID, v.Provider, v.Currency, v.ProviderRef, v.Status, v.CreatedBy).Scan(&v.CreatedAt)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

func (r *Repository) FindVerification(ctx context.Context, teamID, id uuid.UUID) (*Verification, error) {
	var v Verification
	err := r.queries.Executor(ctx).QueryRow(ctx,
		`SELECT `+verificationColumns+` FROM card_verifications WHERE id = $1 AND team_id = $2`, id, teamID).
		Scan(&v.ID, &v.TeamID, &v.Provider, &v.Currency, &v.ProviderRef, &v.Status, &v.PaymentMethodID, &v.FailureReason,
			&v.CreatedBy, &v.CreatedAt, &v.CompletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrorUnknownVerify
	}
	if err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}

	return &v, nil
}

func (r *Repository) SaveVerification(ctx context.Context, v *Verification) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		UPDATE card_verifications
		SET status = $2, payment_method_id = $3, failure_reason = $4, completed_at = $5
		WHERE id = $1`, v.ID, v.Status, v.PaymentMethodID, v.FailureReason, v.CompletedAt)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Routes
// ---------------------------------------------------------------------------

// FindRoute answers the country's row or the default one.
func (r *Repository) FindRoute(ctx context.Context, country string) (*Route, error) {
	var route Route
	err := r.queries.Executor(ctx).QueryRow(ctx, `
		SELECT country, provider, currency FROM payment_routes
		WHERE country IN ($1, $2)
		ORDER BY (country = $1) DESC
		LIMIT 1`, country, RouteDefault).Scan(&route.Country, &route.Provider, &route.Currency)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrorNoRoute.Copy().Add("country", country)
	}
	if err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}

	return &route, nil
}
