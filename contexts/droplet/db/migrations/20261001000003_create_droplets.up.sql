CREATE TYPE droplet_status AS ENUM ('provisioning', 'active', 'off', 'failed', 'deleting', 'deleted');

CREATE TABLE droplets (
    id                    UUID PRIMARY KEY,
    project_id            UUID NOT NULL,
    team_id               UUID NOT NULL,
    plan_id               UUID NOT NULL REFERENCES plans (id),
    plan_code             TEXT NOT NULL,
    region_code           TEXT NOT NULL,
    image_code            TEXT NOT NULL,
    hostname              TEXT NOT NULL,
    -- The Plan's price at creation; it never changes for this Droplet.
    hourly_price          NUMERIC(12, 4) NOT NULL,
    monthly_cap_hours     INTEGER NOT NULL DEFAULT 672,
    status                droplet_status NOT NULL,
    provider_id           UUID NOT NULL REFERENCES providers (id),
    provider_ref          TEXT,
    ipv4                  TEXT,
    ipv6                  TEXT,
    ssh_keys              JSONB NOT NULL DEFAULT '[]'::jsonb,
    pending_action        TEXT,
    failure_reason        TEXT,
    created_by            UUID NOT NULL,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    active_at             TIMESTAMPTZ,
    deletion_requested_at TIMESTAMPTZ,
    deleted_at            TIMESTAMPTZ
);

CREATE INDEX droplets_project_id_idx ON droplets (project_id, created_at DESC);
CREATE INDEX droplets_transitional_idx ON droplets (status) WHERE status IN ('provisioning', 'deleting') OR pending_action IS NOT NULL;

-- One row per transition; the billing phase sums the active/off intervals.
CREATE TABLE droplet_status_history (
    id          BIGSERIAL PRIMARY KEY,
    droplet_id  UUID NOT NULL REFERENCES droplets (id),
    from_status droplet_status,
    to_status   droplet_status NOT NULL,
    at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    reason      TEXT NOT NULL DEFAULT ''
);

CREATE INDEX droplet_status_history_droplet_id_idx ON droplet_status_history (droplet_id, at);
