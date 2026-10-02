-- One Billing profile per Team: who the Invoice is made out to. The tax
-- identifiers are kept as the e-invoice issuer will ask for them (ADR-0006).
CREATE TABLE billing_profiles (
    team_id       UUID PRIMARY KEY,
    account_type  TEXT NOT NULL CHECK (account_type IN ('individual', 'company')),
    legal_name    TEXT NOT NULL,
    country       CHAR(2) NOT NULL,
    address_line1 TEXT NOT NULL,
    address_line2 TEXT NOT NULL DEFAULT '',
    city          TEXT NOT NULL,
    postal_code   TEXT NOT NULL DEFAULT '',
    tax_id        TEXT NOT NULL DEFAULT '',
    tax_office    TEXT NOT NULL DEFAULT '',
    vat_id        TEXT NOT NULL DEFAULT '',
    invoice_email TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
