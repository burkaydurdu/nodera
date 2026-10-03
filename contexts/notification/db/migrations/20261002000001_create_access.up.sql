-- Read model fed by the customer context's events (docs/adr/0004,
-- docs/adr/0008): the Teams a Notification can be about and who is in them
-- with which Role, so the members allowed to see a thing are the ones told
-- about it. Contact details ride the membership for the email Channel.
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
