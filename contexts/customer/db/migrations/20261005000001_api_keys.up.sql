CREATE TABLE api_keys (
    -- The public part of nd_<id>.<secret>; the secret is kept only as the
    -- SHA-256 hex in secret_hash.
    id          TEXT PRIMARY KEY CHECK (id ~ '^[0-9A-Za-z]{12}$'),
    project_id  UUID NOT NULL REFERENCES projects (id),
    team_id     UUID NOT NULL REFERENCES teams (id),
    name        TEXT NOT NULL,
    secret_hash TEXT NOT NULL,
    permissions TEXT[] NOT NULL,
    created_by  UUID NOT NULL REFERENCES users (id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at  TIMESTAMPTZ
);

-- A Project's active keys, newest first: the list, and the count the limit of
-- 20 is checked against.
CREATE INDEX api_keys_project_active_idx ON api_keys (project_id, created_at DESC) WHERE revoked_at IS NULL;
