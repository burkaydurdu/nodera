package invoice

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

type Repository struct {
	queries *postgresql.Queries
}

func NewRepository(queries *postgresql.Queries) *Repository {
	return &Repository{queries: queries}
}

const invoiceColumns = `id, team_id, kind, status, number, period_start, period_end, window_end, currency,
	subtotal, tax_total, credit_applied, total_due, settlement_currency, settlement_amount, exchange_rate,
	exchange_rate_date, locale, profile_snapshot, issued_at, paid_at, pdf_key, einvoice_ref, failed_at, dunning_step, notified_at`

func scanInvoice(row pgx.Row) (*Invoice, error) {
	var (
		inv        Invoice
		settlement decimal.NullDecimal
		rate       decimal.NullDecimal
		snapshot   []byte
	)
	err := row.Scan(&inv.ID, &inv.TeamID, &inv.Kind, &inv.Status, &inv.Number, &inv.PeriodStart, &inv.PeriodEnd, &inv.WindowEnd,
		&inv.Currency, &inv.Subtotal, &inv.TaxTotal, &inv.CreditApplied, &inv.TotalDue, &inv.SettlementCurrency, &settlement, &rate,
		&inv.ExchangeRateDate, &inv.Locale, &snapshot, &inv.IssuedAt, &inv.PaidAt, &inv.PDFKey, &inv.EInvoiceRef, &inv.FailedAt, &inv.DunningStep, &inv.NotifiedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrorNotFound
	}
	if err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}
	if settlement.Valid {
		inv.SettlementAmount = &settlement.Decimal
	}
	if rate.Valid {
		inv.ExchangeRate = &rate.Decimal
	}
	if err := json.Unmarshal(snapshot, &inv.Profile); err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}

	return &inv, nil
}

func (r *Repository) scanInvoices(rows pgx.Rows, err error) ([]*Invoice, error) {
	if err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}
	defer rows.Close()

	out := make([]*Invoice, 0)
	for rows.Next() {
		inv, err := scanInvoice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	if err := rows.Err(); err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}

	return out, nil
}

// NextNumber takes the year's next sequence value; the upsert holds the
// year's row until the transaction ends, so numbers are gapless per commit
// order.
func (r *Repository) NextNumber(ctx context.Context, year int) (int64, error) {
	var next int64
	err := r.queries.Executor(ctx).QueryRow(ctx, `
		INSERT INTO invoice_numbers (year, last_value) VALUES ($1, 1)
		ON CONFLICT (year) DO UPDATE SET last_value = invoice_numbers.last_value + 1
		RETURNING last_value`, year).Scan(&next)
	if err != nil {
		return 0, ErrorCannotStore.Copy().SetError(err)
	}

	return next, nil
}

func (r *Repository) Create(ctx context.Context, inv *Invoice) error {
	snapshot, err := json.Marshal(inv.Profile)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}
	executor := r.queries.Executor(ctx)
	err = executor.QueryRow(ctx, `
		INSERT INTO invoices (team_id, kind, status, number, period_start, period_end, window_end, currency, subtotal, tax_total,
			credit_applied, total_due, settlement_currency, settlement_amount, exchange_rate, exchange_rate_date, locale,
			profile_snapshot, issued_at, paid_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
		RETURNING id`,
		inv.TeamID, inv.Kind, inv.Status, inv.Number, inv.PeriodStart, inv.PeriodEnd, inv.WindowEnd, inv.Currency, inv.Subtotal,
		inv.TaxTotal, inv.CreditApplied, inv.TotalDue, inv.SettlementCurrency, nullable(inv.SettlementAmount), nullable(inv.ExchangeRate),
		inv.ExchangeRateDate, inv.Locale, snapshot, inv.IssuedAt, inv.PaidAt).Scan(&inv.ID)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	for i, l := range inv.Lines {
		if _, err := executor.Exec(ctx, `
			INSERT INTO invoice_lines (invoice_id, position, kind, droplet_id, hostname, plan_code, hours, hourly_price,
				project_id, project_name, package_code, amount)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`, lineValues(inv.ID, i, l)...); err != nil {
			return ErrorCannotStore.Copy().SetError(err)
		}
	}
	for i, t := range inv.TaxLines {
		if _, err := executor.Exec(ctx, `
			INSERT INTO invoice_tax_lines (invoice_id, position, label_key, label, rate_bps, base, amount)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			inv.ID, i, t.LabelKey, t.Label, t.RateBps, t.Base, t.Amount); err != nil {
			return ErrorCannotStore.Copy().SetError(err)
		}
	}

	return nil
}

func (r *Repository) ApplyCredit(ctx context.Context, teamID, invoiceID uuid.UUID, amount decimal.Decimal) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		INSERT INTO credit_ledger (team_id, amount, invoice_id, reason) VALUES ($1, $2, $3, 'invoice')`,
		teamID, amount.Neg(), invoiceID)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

func (r *Repository) GrantCredit(ctx context.Context, teamID uuid.UUID, amount decimal.Decimal, reason string) error {
	_, err := r.queries.Executor(ctx).Exec(ctx,
		`INSERT INTO credit_ledger (team_id, amount, reason) VALUES ($1, $2, $3)`, teamID, amount, reason)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

func (r *Repository) CreditBalance(ctx context.Context, teamID uuid.UUID) (decimal.Decimal, error) {
	var balance decimal.Decimal
	err := r.queries.Executor(ctx).QueryRow(ctx,
		`SELECT COALESCE(sum(amount), 0) FROM credit_ledger WHERE team_id = $1`, teamID).Scan(&balance)
	if err != nil {
		return decimal.Zero, ErrorCannotRead.Copy().SetError(err)
	}

	return balance, nil
}

func (r *Repository) PeriodInvoiceExists(ctx context.Context, teamID uuid.UUID, periodStart time.Time) (bool, error) {
	var n int
	err := r.queries.Executor(ctx).QueryRow(ctx,
		`SELECT count(*) FROM invoices WHERE team_id = $1 AND period_start = $2 AND kind = $3`, teamID, periodStart, KindPeriod).Scan(&n)
	if err != nil {
		return false, ErrorCannotRead.Copy().SetError(err)
	}

	return n > 0, nil
}

// billedLines selects the lines of one kind that the Team's live Invoices of
// a Period carry; it takes the Team, the Period's start, StatusVoid and the
// kind as $1 to $4.
const billedLines = `
	FROM invoice_lines l JOIN invoices i ON i.id = l.invoice_id
	WHERE i.team_id = $1 AND i.period_start = $2 AND i.status <> $3 AND l.kind = $4`

func (r *Repository) BilledHours(ctx context.Context, teamID uuid.UUID, periodStart time.Time) (map[uuid.UUID]int, error) {
	return scanSums[int](r.queries.Executor(ctx).Query(ctx,
		`SELECT l.droplet_id, sum(l.hours)`+billedLines+` GROUP BY l.droplet_id`, teamID, periodStart, StatusVoid, LineDroplet))
}

func (r *Repository) BilledMail(ctx context.Context, teamID uuid.UUID, periodStart time.Time) (map[uuid.UUID]decimal.Decimal, error) {
	return scanSums[decimal.Decimal](r.queries.Executor(ctx).Query(ctx,
		`SELECT l.project_id, sum(l.amount)`+billedLines+` GROUP BY l.project_id`, teamID, periodStart, StatusVoid, LineMail))
}

func scanSums[V any](rows pgx.Rows, err error) (map[uuid.UUID]V, error) {
	if err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}
	defer rows.Close()

	sums := make(map[uuid.UUID]V)
	for rows.Next() {
		var (
			id  uuid.UUID
			sum V
		)
		if err := rows.Scan(&id, &sum); err != nil {
			return nil, ErrorCannotRead.Copy().SetError(err)
		}
		sums[id] = sum
	}
	if err := rows.Err(); err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}

	return sums, nil
}

// Find answers an Invoice with its lines and Tax lines; a teamID confines
// it to that Team, lock holds it for the transaction.
func (r *Repository) Find(ctx context.Context, id uuid.UUID, teamID *uuid.UUID, lock bool) (*Invoice, error) {
	query := `SELECT ` + invoiceColumns + ` FROM invoices WHERE id = $1 AND ($2::uuid IS NULL OR team_id = $2)`
	if lock {
		query += ` FOR UPDATE`
	}
	inv, err := scanInvoice(r.queries.Executor(ctx).QueryRow(ctx, query, id, teamID))
	if err != nil {
		return nil, err
	}

	return inv, r.loadLines(ctx, inv)
}

// FindByTeamID answers the Team's Invoices without lines, newest first.
func (r *Repository) FindByTeamID(ctx context.Context, teamID uuid.UUID) ([]*Invoice, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx,
		`SELECT `+invoiceColumns+` FROM invoices WHERE team_id = $1 ORDER BY issued_at DESC, number DESC`, teamID)

	return r.scanInvoices(rows, err)
}

func (r *Repository) FindFailed(ctx context.Context, teamID *uuid.UUID) ([]*Invoice, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx, `
		SELECT `+invoiceColumns+` FROM invoices
		WHERE status = $1 AND ($2::uuid IS NULL OR team_id = $2)
		ORDER BY failed_at, issued_at, id`, StatusPaymentFailed, teamID)

	return r.scanInvoices(rows, err)
}

func (r *Repository) Save(ctx context.Context, inv *Invoice) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `
		UPDATE invoices
		SET status = $2, paid_at = $3, failed_at = $4, dunning_step = $5, updated_at = now()
		WHERE id = $1`, inv.ID, inv.Status, inv.PaidAt, inv.FailedAt, inv.DunningStep)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

func (r *Repository) SetPDFKey(ctx context.Context, id uuid.UUID, key string) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `UPDATE invoices SET pdf_key = $2, updated_at = now() WHERE id = $1`, id, key)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

func (r *Repository) SetNotified(ctx context.Context, id uuid.UUID, at time.Time) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `UPDATE invoices SET notified_at = $2, updated_at = now() WHERE id = $1`, id, at)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

// FindIssuedBefore answers the issued Invoices older than the instant,
// oldest first, for the sweep.
func (r *Repository) FindIssuedBefore(ctx context.Context, before time.Time, limit int) ([]*Invoice, error) {
	rows, err := r.queries.Executor(ctx).Query(ctx,
		`SELECT `+invoiceColumns+` FROM invoices WHERE status = $1 AND issued_at < $2 ORDER BY issued_at, id LIMIT $3`,
		StatusIssued, before, limit)

	return r.scanInvoices(rows, err)
}

func (r *Repository) SetEInvoiceRef(ctx context.Context, id uuid.UUID, ref string) error {
	_, err := r.queries.Executor(ctx).Exec(ctx, `UPDATE invoices SET einvoice_ref = $2, updated_at = now() WHERE id = $1`, id, ref)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}

func (r *Repository) UnpaidExists(ctx context.Context, teamID uuid.UUID) (bool, error) {
	var n int
	err := r.queries.Executor(ctx).QueryRow(ctx,
		`SELECT count(*) FROM invoices WHERE team_id = $1 AND status IN ($2, $3)`, teamID, StatusIssued, StatusPaymentFailed).Scan(&n)
	if err != nil {
		return false, ErrorCannotRead.Copy().SetError(err)
	}

	return n > 0, nil
}

func (r *Repository) loadLines(ctx context.Context, inv *Invoice) error {
	executor := r.queries.Executor(ctx)
	rows, err := executor.Query(ctx, `
		SELECT kind, droplet_id, hostname, plan_code, hours, hourly_price, project_id, project_name, package_code, amount
		FROM invoice_lines WHERE invoice_id = $1 ORDER BY position`, inv.ID)
	if err != nil {
		return ErrorCannotRead.Copy().SetError(err)
	}
	inv.Lines = make([]Line, 0)
	for rows.Next() {
		l, err := scanLine(rows)
		if err != nil {
			rows.Close()

			return err
		}
		inv.Lines = append(inv.Lines, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return ErrorCannotRead.Copy().SetError(err)
	}

	rows, err = executor.Query(ctx, `
		SELECT label_key, label, rate_bps, base, amount
		FROM invoice_tax_lines WHERE invoice_id = $1 ORDER BY position`, inv.ID)
	if err != nil {
		return ErrorCannotRead.Copy().SetError(err)
	}
	defer rows.Close()
	inv.TaxLines = make([]TaxLine, 0)
	for rows.Next() {
		var t TaxLine
		if err := rows.Scan(&t.LabelKey, &t.Label, &t.RateBps, &t.Base, &t.Amount); err != nil {
			return ErrorCannotRead.Copy().SetError(err)
		}
		inv.TaxLines = append(inv.TaxLines, t)
	}
	if err := rows.Err(); err != nil {
		return ErrorCannotRead.Copy().SetError(err)
	}

	return nil
}

// lineValues answers a line's row: its own kind's columns and NULL for the
// other kind's, as the table's checks require.
func lineValues(invoiceID uuid.UUID, position int, l Line) []any {
	if l.Kind == LineMail {
		return []any{invoiceID, position, l.Kind, nil, nil, nil, nil, nil, l.ProjectID, l.ProjectName, l.PackageCode, l.Amount}
	}

	return []any{invoiceID, position, l.Kind, l.DropletID, l.Hostname, l.PlanCode, l.Hours, l.HourlyPrice, nil, nil, nil, l.Amount}
}

func scanLine(row pgx.Row) (Line, error) {
	var (
		l           Line
		dropletID   *uuid.UUID
		hostname    *string
		planCode    *string
		hours       *int
		hourlyPrice decimal.NullDecimal
		projectID   *uuid.UUID
		projectName *string
		packageCode *string
	)
	if err := row.Scan(&l.Kind, &dropletID, &hostname, &planCode, &hours, &hourlyPrice, &projectID, &projectName, &packageCode, &l.Amount); err != nil {
		return Line{}, ErrorCannotRead.Copy().SetError(err)
	}
	if l.Kind == LineMail {
		l.ProjectID, l.ProjectName, l.PackageCode = *projectID, *projectName, *packageCode

		return l, nil
	}
	l.DropletID, l.Hostname, l.PlanCode, l.Hours, l.HourlyPrice = *dropletID, *hostname, *planCode, *hours, hourlyPrice.Decimal

	return l, nil
}

func nullable(d *decimal.Decimal) decimal.NullDecimal {
	if d == nil {
		return decimal.NullDecimal{}
	}

	return decimal.NullDecimal{Decimal: *d, Valid: true}
}
