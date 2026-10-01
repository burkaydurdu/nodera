-- The membership events arrive on separate queues with no ordering, so each
-- row remembers the time of the event that last shaped it and ignores older
-- ones. A removal is a tombstone, not a delete: a late copy of an earlier
-- TeamMemberAdded would otherwise recreate the row.
ALTER TABLE team_access
    ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN removed_at TIMESTAMPTZ NULL;

ALTER TABLE team_access ALTER COLUMN updated_at DROP DEFAULT;
