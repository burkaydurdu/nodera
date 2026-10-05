CREATE TABLE suppressions (
    id         UUID PRIMARY KEY,
    project_id UUID NOT NULL,
    address    TEXT NOT NULL,
    reason     TEXT NOT NULL CHECK (reason IN ('bounce', 'complaint')),
    message_id UUID,
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (project_id, address)
);

CREATE INDEX suppressions_project_created_idx ON suppressions (project_id, created_at DESC, id DESC);
