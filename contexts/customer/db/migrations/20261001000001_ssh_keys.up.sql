CREATE TABLE ssh_keys (
    id          UUID PRIMARY KEY DEFAULT uuidv7(),
    team_id     UUID NOT NULL REFERENCES teams (id),
    name        TEXT NOT NULL,
    public_key  TEXT NOT NULL,
    fingerprint TEXT NOT NULL,
    created_by  UUID NOT NULL REFERENCES users (id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX ssh_keys_team_name_unique ON ssh_keys (team_id, lower(name));
CREATE UNIQUE INDEX ssh_keys_team_fingerprint_unique ON ssh_keys (team_id, fingerprint);
