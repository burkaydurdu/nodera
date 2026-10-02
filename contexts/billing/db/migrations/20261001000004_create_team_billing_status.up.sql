-- Whether a Team may spend. Absent row = no_payment_method. The nullable
-- columns are filled by collection: the open Invoice behind a payment_failed
-- or suspended state, when the Team was suspended, when its Droplets will be
-- deleted and when the next Dunning retry runs.
CREATE TABLE team_billing_status (
    team_id         UUID PRIMARY KEY,
    status          TEXT NOT NULL CHECK (status IN ('no_payment_method', 'active', 'suspended')),
    changed_at      TIMESTAMPTZ NOT NULL,
    open_invoice_id UUID NULL,
    suspended_at    TIMESTAMPTZ NULL,
    deletion_at     TIMESTAMPTZ NULL,
    next_retry_at   TIMESTAMPTZ NULL,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
