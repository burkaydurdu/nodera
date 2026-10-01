DELETE FROM team_access WHERE removed_at IS NOT NULL;

ALTER TABLE team_access
    DROP COLUMN removed_at,
    DROP COLUMN updated_at;
