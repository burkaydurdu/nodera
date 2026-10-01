CREATE TABLE projects (
    id          UUID PRIMARY KEY DEFAULT uuidv7(),
    team_id     UUID NOT NULL REFERENCES teams (id),
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ
);

CREATE UNIQUE INDEX projects_team_name_unique ON projects (team_id, lower(name)) WHERE deleted_at IS NULL;
