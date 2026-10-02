DROP INDEX IF EXISTS droplets_billed_idx;
DROP INDEX IF EXISTS droplets_team_id_idx;

ALTER TABLE droplets
    DROP COLUMN IF EXISTS suspended_power_off_at,
    DROP COLUMN IF EXISTS billing_stopped_published_at,
    DROP COLUMN IF EXISTS billing_started_published_at,
    DROP COLUMN IF EXISTS failed_at;

DROP TABLE IF EXISTS team_billing;
