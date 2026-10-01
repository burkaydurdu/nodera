-- Read model fed by the customer context's events (docs/adr/0004).
CREATE TABLE project_access (
    project_id UUID PRIMARY KEY,
    team_id    UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX project_access_team_id_idx ON project_access (team_id);

CREATE TABLE team_access (
    team_id    UUID NOT NULL,
    user_id    UUID NOT NULL,
    role       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (team_id, user_id)
);

CREATE INDEX team_access_user_id_idx ON team_access (user_id);
