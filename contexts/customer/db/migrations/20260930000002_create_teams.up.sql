CREATE TYPE team_role AS ENUM ('owner');

CREATE TABLE teams (
    id            UUID PRIMARY KEY DEFAULT uuidv7(),
    name          TEXT NOT NULL,
    owner_user_id UUID NOT NULL REFERENCES users (id),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at    TIMESTAMPTZ
);

CREATE INDEX teams_owner_user_id_idx ON teams (owner_user_id);

CREATE TABLE team_members (
    team_id    UUID NOT NULL REFERENCES teams (id),
    user_id    UUID NOT NULL REFERENCES users (id),
    role       team_role NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (team_id, user_id)
);

CREATE INDEX team_members_user_id_idx ON team_members (user_id);
