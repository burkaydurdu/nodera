-- Read model fed by the customer context's events (docs/adr/0004): the Teams
-- billing invoices and the members who may see or change its money matters.
-- Contact details ride the membership so invoice emails can reach every owner
-- without asking the customer context.
CREATE TABLE teams (
    team_id    UUID PRIMARY KEY,
    name       TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL
);

-- The membership events arrive on separate queues with no ordering, so each
-- row remembers the time of the event that last shaped it and ignores older
-- ones. A removal is a tombstone, not a delete: a late copy of an earlier
-- TeamMemberAdded would otherwise recreate the row.
CREATE TABLE team_members (
    team_id    UUID NOT NULL,
    user_id    UUID NOT NULL,
    role       TEXT NOT NULL,
    email      TEXT NOT NULL DEFAULT '',
    full_name  TEXT NOT NULL DEFAULT '',
    locale     TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL,
    removed_at TIMESTAMPTZ NULL,
    PRIMARY KEY (team_id, user_id)
);

CREATE INDEX team_members_user_id_idx ON team_members (user_id);
