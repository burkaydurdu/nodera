-- Read models fed by the customer and billing contexts' events (docs/adr/0004).
CREATE TABLE project_access (
    project_id UUID PRIMARY KEY,
    team_id    UUID NOT NULL,
    name       TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX project_access_team_id_idx ON project_access (team_id);

-- updated_at is the time of the event that last shaped the row, so an older
-- event arriving later changes nothing; removed_at keeps a removal as a
-- tombstone a late TeamMemberAdded cannot undo.
CREATE TABLE team_access (
    team_id    UUID NOT NULL,
    user_id    UUID NOT NULL,
    role       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL,
    removed_at TIMESTAMPTZ,
    PRIMARY KEY (team_id, user_id)
);

CREATE INDEX team_access_user_id_idx ON team_access (user_id);

CREATE TABLE team_billing (
    team_id    UUID PRIMARY KEY,
    status     TEXT NOT NULL CHECK (status IN ('no_payment_method', 'active', 'suspended')),
    changed_at TIMESTAMPTZ NOT NULL
);
