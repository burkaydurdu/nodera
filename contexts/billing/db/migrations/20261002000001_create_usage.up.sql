-- One row per Droplet, fed by the droplet context's DropletBillingStarted and
-- DropletBillingStopped events. A Droplet's hours are derived from the row on
-- read; nothing here is ever priced or summed in SQL. A row whose started_at
-- is NULL is a tombstone: the stop arrived first, and it is ignored for usage
-- until (and unless) the start arrives.
CREATE TABLE usage (
    droplet_id   UUID PRIMARY KEY,
    team_id      UUID NOT NULL,
    project_id   UUID NULL,
    hostname     TEXT NOT NULL DEFAULT '',
    plan_code    TEXT NOT NULL DEFAULT '',
    hourly_price NUMERIC(12, 4) NULL CHECK (hourly_price >= 0),
    started_at   TIMESTAMPTZ NULL,
    stopped_at   TIMESTAMPTZ NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX usage_team_id_idx ON usage (team_id) WHERE started_at IS NOT NULL;
