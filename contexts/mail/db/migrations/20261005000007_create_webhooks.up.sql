-- secret is sealed with AES-256-GCM like the DKIM keys: it must be read back to
-- sign every delivery.
CREATE TABLE webhooks (
    id         UUID PRIMARY KEY,
    project_id UUID NOT NULL,
    url        TEXT NOT NULL,
    events     TEXT[] NOT NULL,
    active     BOOLEAN NOT NULL DEFAULT true,
    secret     BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX webhooks_project_id_idx ON webhooks (project_id);

-- The batch a delivery sends is frozen in body, so every retry signs and sends
-- the same bytes.
CREATE TABLE webhook_deliveries (
    id               UUID PRIMARY KEY,
    webhook_id       UUID NOT NULL REFERENCES webhooks (id) ON DELETE CASCADE,
    status           TEXT NOT NULL CHECK (status IN ('pending', 'succeeded', 'failed')),
    attempts         INTEGER NOT NULL DEFAULT 0,
    event_count      INTEGER NOT NULL,
    body             JSONB NOT NULL,
    next_attempt_at  TIMESTAMPTZ,
    last_attempt_at  TIMESTAMPTZ,
    last_status_code INTEGER,
    last_error       TEXT,
    created_at       TIMESTAMPTZ NOT NULL
);

CREATE INDEX webhook_deliveries_due_idx ON webhook_deliveries (next_attempt_at) WHERE status = 'pending';
CREATE INDEX webhook_deliveries_webhook_idx ON webhook_deliveries (webhook_id, created_at DESC, id DESC);

-- Message events waiting to be batched into a delivery; seq keeps them in the
-- order they happened when several share an instant.
CREATE TABLE webhook_events (
    id          UUID PRIMARY KEY,
    seq         BIGINT GENERATED ALWAYS AS IDENTITY,
    webhook_id  UUID NOT NULL REFERENCES webhooks (id) ON DELETE CASCADE,
    payload     JSONB NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL,
    delivery_id UUID REFERENCES webhook_deliveries (id) ON DELETE SET NULL
);

CREATE INDEX webhook_events_pending_idx ON webhook_events (webhook_id, seq) WHERE delivery_id IS NULL;
