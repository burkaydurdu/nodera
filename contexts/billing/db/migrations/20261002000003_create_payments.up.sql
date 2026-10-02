-- One attempt to collect an Invoice. The idempotency key goes to the provider
-- with the charge, so a retried call never charges twice. A confirmation
-- link carries an HMAC token checked against confirm_expires_at.
CREATE TABLE payments (
    id                 UUID PRIMARY KEY,
    invoice_id         UUID NOT NULL REFERENCES invoices (id),
    team_id            UUID NOT NULL,
    payment_method_id  UUID NULL REFERENCES payment_methods (id),
    provider           TEXT NOT NULL DEFAULT '',
    attempt            INTEGER NOT NULL CHECK (attempt > 0),
    idempotency_key    TEXT NOT NULL UNIQUE,
    trigger            TEXT NOT NULL CHECK (trigger IN ('issue', 'dunning', 'pay_now')),
    amount             NUMERIC(16, 2) NOT NULL,
    currency           CHAR(3) NOT NULL,
    status             TEXT NOT NULL CHECK (status IN ('pending', 'succeeded', 'failed', 'confirmation_required')),
    provider_ref       TEXT NOT NULL DEFAULT '',
    failure_reason     TEXT NOT NULL DEFAULT '',
    confirmation_url   TEXT NOT NULL DEFAULT '',
    confirm_expires_at TIMESTAMPTZ NULL,
    created_at         TIMESTAMPTZ NOT NULL,
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at       TIMESTAMPTZ NULL,
    UNIQUE (invoice_id, attempt)
);

CREATE INDEX payments_open_idx ON payments (created_at) WHERE status = 'pending';
CREATE INDEX payments_provider_ref_idx ON payments (provider, provider_ref) WHERE provider_ref <> '';
