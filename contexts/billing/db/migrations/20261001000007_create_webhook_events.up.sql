-- Every provider webhook that passed its signature check, stored once by the
-- provider's own event id so a replay is a no-op. Reconciliation reads the
-- provider's API, never the event body, before it touches an Invoice.
CREATE TABLE webhook_events (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider             TEXT NOT NULL,
    provider_event_id    TEXT NOT NULL,
    kind                 TEXT NOT NULL,
    provider_payment_ref TEXT NOT NULL DEFAULT '',
    status               TEXT NOT NULL DEFAULT '',
    body                 BYTEA NOT NULL,
    received_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at         TIMESTAMPTZ NULL,
    UNIQUE (provider, provider_event_id)
);

CREATE INDEX webhook_events_pending_idx ON webhook_events (received_at) WHERE processed_at IS NULL;
