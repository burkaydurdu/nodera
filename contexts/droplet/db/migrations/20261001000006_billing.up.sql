-- Billing status per Team, learned from the billing context's events and
-- never queried (docs/adr/0006). A Team without a row has no payment method.
CREATE TABLE team_billing (
    team_id    UUID PRIMARY KEY,
    status     TEXT NOT NULL,
    changed_at TIMESTAMPTZ NOT NULL
);

-- The meter's boundaries are announced exactly once each: the claim is made
-- in the same transaction as the transition and the event goes out after the
-- commit, so a redelivered poll cannot announce twice. failed_at is when a
-- Droplet that was running stopped counting; suspended_power_off_at marks the
-- Droplets this context powered off for a suspension, so recovery powers on
-- exactly those.
ALTER TABLE droplets
    ADD COLUMN failed_at                    TIMESTAMPTZ,
    ADD COLUMN billing_started_published_at TIMESTAMPTZ,
    ADD COLUMN billing_stopped_published_at TIMESTAMPTZ,
    ADD COLUMN suspended_power_off_at       TIMESTAMPTZ;

CREATE INDEX droplets_team_id_idx ON droplets (team_id) WHERE status <> 'deleted';
CREATE INDEX droplets_billed_idx ON droplets (created_at) WHERE active_at IS NOT NULL;
