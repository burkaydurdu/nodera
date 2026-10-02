package profile

import (
	"context"
	"errors"

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

const selectColumns = `team_id, account_type, legal_name, country, address_line1, address_line2, city, postal_code,
	tax_id, tax_office, vat_id, invoice_email, created_at, updated_at`

func (r *Repository) FindByTeamID(ctx context.Context, teamID uuid.UUID) (*Profile, error) {
	var p Profile
	err := r.queries.Executor(ctx).QueryRow(ctx, `SELECT `+selectColumns+` FROM billing_profiles WHERE team_id = $1`, teamID).
		Scan(&p.TeamID, &p.AccountType, &p.LegalName, &p.Country, &p.AddressLine1, &p.AddressLine2, &p.City, &p.PostalCode,
			&p.TaxID, &p.TaxOffice, &p.VATID, &p.InvoiceEmail, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrorNotFound
	}
	if err != nil {
		return nil, ErrorCannotRead.Copy().SetError(err)
	}

	return &p, nil
}

// Upsert writes the whole Profile; the row's timestamps come back on it.
func (r *Repository) Upsert(ctx context.Context, p *Profile) error {
	err := r.queries.Executor(ctx).QueryRow(ctx, `
		INSERT INTO billing_profiles (team_id, account_type, legal_name, country, address_line1, address_line2, city,
			postal_code, tax_id, tax_office, vat_id, invoice_email)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (team_id) DO UPDATE SET
			account_type = EXCLUDED.account_type, legal_name = EXCLUDED.legal_name, country = EXCLUDED.country,
			address_line1 = EXCLUDED.address_line1, address_line2 = EXCLUDED.address_line2, city = EXCLUDED.city,
			postal_code = EXCLUDED.postal_code, tax_id = EXCLUDED.tax_id, tax_office = EXCLUDED.tax_office,
			vat_id = EXCLUDED.vat_id, invoice_email = EXCLUDED.invoice_email, updated_at = now()
		RETURNING created_at, updated_at`,
		p.TeamID, p.AccountType, p.LegalName, p.Country, p.AddressLine1, p.AddressLine2, p.City,
		p.PostalCode, p.TaxID, p.TaxOffice, p.VATID, p.InvoiceEmail).Scan(&p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return ErrorCannotStore.Copy().SetError(err)
	}

	return nil
}
