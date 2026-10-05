-- Logs keep metadata only; the content waits in message_spool until the MTA
-- has accepted it and is then deleted.
CREATE TABLE messages (
    id                UUID PRIMARY KEY,
    project_id        UUID NOT NULL,
    team_id           UUID NOT NULL,
    sending_domain_id UUID NOT NULL,
    api_key_id        TEXT NOT NULL,
    source            TEXT NOT NULL CHECK (source IN ('api', 'smtp')),
    from_address      TEXT NOT NULL,
    from_name         TEXT NOT NULL DEFAULT '',
    subject           TEXT NOT NULL,
    message_id_header TEXT NOT NULL,
    tags              TEXT[] NOT NULL DEFAULT '{}',
    size_bytes        INTEGER NOT NULL,
    idempotency_key   TEXT,
    created_at        TIMESTAMPTZ NOT NULL,
    handed_over_at    TIMESTAMPTZ
);

CREATE INDEX messages_project_created_idx ON messages (project_id, created_at DESC, id DESC);
CREATE INDEX messages_idempotency_idx ON messages (project_id, idempotency_key, created_at) WHERE idempotency_key IS NOT NULL;
CREATE INDEX messages_created_idx ON messages (created_at);

-- position is the recipient's index in the return path, so an asynchronous
-- bounce finds its recipient again.
CREATE TABLE message_recipients (
    message_id UUID NOT NULL REFERENCES messages (id) ON DELETE CASCADE,
    position   INTEGER NOT NULL,
    address    TEXT NOT NULL,
    name       TEXT NOT NULL DEFAULT '',
    kind       TEXT NOT NULL CHECK (kind IN ('to', 'cc', 'bcc')),
    status     TEXT NOT NULL CHECK (status IN ('accepted', 'delivered', 'deferred', 'bounced', 'suppressed', 'complained')),
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (message_id, position)
);

CREATE INDEX message_recipients_address_idx ON message_recipients (address);

CREATE TABLE message_events (
    id            UUID PRIMARY KEY,
    message_id    UUID NOT NULL REFERENCES messages (id) ON DELETE CASCADE,
    position      INTEGER NOT NULL,
    type          TEXT NOT NULL CHECK (type IN ('accepted', 'delivered', 'deferred', 'bounced', 'suppressed', 'complained')),
    occurred_at   TIMESTAMPTZ NOT NULL,
    response_code INTEGER,
    enhanced_code TEXT,
    response_text TEXT,
    bounce_class  TEXT,
    UNIQUE (message_id, position, type, occurred_at)
);

-- envelope is fixed at acceptance (return path, recipients, signing domain),
-- so handing over never depends on rows that may change in between.
CREATE TABLE message_spool (
    message_id      UUID PRIMARY KEY REFERENCES messages (id) ON DELETE CASCADE,
    content         BYTEA NOT NULL,
    envelope        JSONB NOT NULL,
    attempts        INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL,
    last_error      TEXT
);

CREATE INDEX message_spool_next_attempt_idx ON message_spool (next_attempt_at);
