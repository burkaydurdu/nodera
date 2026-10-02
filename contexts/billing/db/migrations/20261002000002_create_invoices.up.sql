-- Invoice numbers run per calendar year; the row is the lock that keeps two
-- concurrent issues from taking the same number.
CREATE TABLE invoice_numbers (
    year       INTEGER PRIMARY KEY,
    last_value BIGINT NOT NULL
);

-- A Team's statement for a Period (kind period) or for the usage that crossed
-- its Threshold (kind threshold). The draft is never stored: it is computed on
-- read. Money is numeric in USD; the TRY settlement is fixed with the rate of
-- the issue date (ADR-0006). The profile snapshot is what the e-invoice
-- issuer will ask for, as it was on the issue date.
CREATE TABLE invoices (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id             UUID NOT NULL,
    kind                TEXT NOT NULL CHECK (kind IN ('period', 'threshold')),
    status              TEXT NOT NULL CHECK (status IN ('issued', 'paid', 'payment_failed', 'void')),
    number              TEXT NOT NULL UNIQUE,
    period_start        TIMESTAMPTZ NOT NULL,
    period_end          TIMESTAMPTZ NOT NULL,
    window_end          TIMESTAMPTZ NOT NULL,
    currency            CHAR(3) NOT NULL,
    subtotal            NUMERIC(14, 2) NOT NULL,
    tax_total           NUMERIC(14, 2) NOT NULL,
    credit_applied      NUMERIC(14, 2) NOT NULL,
    total_due           NUMERIC(14, 2) NOT NULL,
    settlement_currency CHAR(3) NULL,
    settlement_amount   NUMERIC(16, 2) NULL,
    exchange_rate       NUMERIC(18, 6) NULL,
    exchange_rate_date  DATE NULL,
    locale              TEXT NOT NULL,
    profile_snapshot    JSONB NOT NULL,
    issued_at           TIMESTAMPTZ NOT NULL,
    paid_at             TIMESTAMPTZ NULL,
    pdf_key             TEXT NULL,
    einvoice_ref        TEXT NULL,
    -- Dunning runs from the first failed Payment; the step is the last one
    -- of the schedule already carried out.
    failed_at           TIMESTAMPTZ NULL,
    dunning_step        SMALLINT NOT NULL DEFAULT 0,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One Period Invoice per Team and Period, so closing a Period twice issues once.
CREATE UNIQUE INDEX invoices_one_period_idx ON invoices (team_id, period_start) WHERE kind = 'period';
CREATE INDEX invoices_team_id_idx ON invoices (team_id, issued_at DESC);
CREATE INDEX invoices_dunning_idx ON invoices (failed_at) WHERE status = 'payment_failed';

CREATE TABLE invoice_lines (
    invoice_id   UUID NOT NULL REFERENCES invoices (id),
    position     INTEGER NOT NULL,
    droplet_id   UUID NOT NULL,
    hostname     TEXT NOT NULL,
    plan_code    TEXT NOT NULL,
    hours        INTEGER NOT NULL CHECK (hours > 0),
    hourly_price NUMERIC(12, 4) NOT NULL,
    amount       NUMERIC(14, 2) NOT NULL,
    PRIMARY KEY (invoice_id, position)
);

CREATE INDEX invoice_lines_droplet_id_idx ON invoice_lines (droplet_id);

-- The label is stored as rendered on the issue date in the Invoice's locale;
-- the key stays for the e-invoice issuer.
CREATE TABLE invoice_tax_lines (
    invoice_id UUID NOT NULL REFERENCES invoices (id),
    position   INTEGER NOT NULL,
    label_key  TEXT NOT NULL,
    label      TEXT NOT NULL,
    rate_bps   INTEGER NOT NULL,
    base       NUMERIC(14, 2) NOT NULL,
    amount     NUMERIC(14, 2) NOT NULL,
    PRIMARY KEY (invoice_id, position)
);

-- Credit is a ledger: a grant is positive, an application to an Invoice is
-- negative and names it. A Team's balance is the sum.
CREATE TABLE credit_ledger (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id    UUID NOT NULL,
    amount     NUMERIC(14, 2) NOT NULL CHECK (amount <> 0),
    invoice_id UUID NULL REFERENCES invoices (id),
    reason     TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX credit_ledger_team_id_idx ON credit_ledger (team_id);
