-- A Project's account in the engine and the root key the storage service acts
-- with (docs/adr/0012). The secret is sealed; it never leaves in clear.
CREATE TABLE project_accounts (
    project_id UUID PRIMARY KEY,
    account_id TEXT NOT NULL,
    access_key TEXT NOT NULL,
    secret_key BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A Storage's Package copies the catalog's size and price when it is chosen,
-- like a Droplet's Locked price; a smaller Package scheduled for the next
-- Period copies its terms when it is scheduled.
--
-- team_suspended and paused_at say what the Storage should be; engine_size_bytes
-- and engine_stopped say what the engine was last made to hold. The reconcile
-- job works on the rows where the two differ.
CREATE TABLE storages (
    id                      UUID PRIMARY KEY,
    project_id              UUID NOT NULL,
    team_id                 UUID NOT NULL,
    region                  TEXT NOT NULL REFERENCES regions (code),
    bucket_name             TEXT NOT NULL,
    status                  TEXT NOT NULL CHECK (status IN ('opening', 'active', 'deleting', 'deleted')),
    package_code            TEXT NOT NULL,
    size_bytes              BIGINT NOT NULL CHECK (size_bytes > 0),
    monthly_price           NUMERIC(12, 2) NOT NULL CHECK (monthly_price >= 0),
    package_started_at      TIMESTAMPTZ,
    scheduled_code          TEXT,
    scheduled_size_bytes    BIGINT,
    scheduled_monthly_price NUMERIC(12, 2),
    scheduled_at            TIMESTAMPTZ,
    paused_at               TIMESTAMPTZ,
    pause_reason            TEXT,
    team_suspended          BOOLEAN NOT NULL DEFAULT false,
    used_bytes              BIGINT NOT NULL DEFAULT 0,
    object_count            BIGINT NOT NULL DEFAULT 0,
    measured_at             TIMESTAMPTZ,
    nearly_full_notified_at TIMESTAMPTZ,
    full_notified_at        TIMESTAMPTZ,
    engine_size_bytes       BIGINT NOT NULL DEFAULT 0,
    engine_stopped          BOOLEAN NOT NULL DEFAULT false,
    traffic_read_until      TIMESTAMPTZ,
    created_by              UUID NOT NULL,
    created_at              TIMESTAMPTZ NOT NULL,
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at              TIMESTAMPTZ,
    purged_at               TIMESTAMPTZ,
    CHECK ((status = 'opening') = (package_started_at IS NULL)),
    CHECK ((scheduled_code IS NULL) = (scheduled_size_bytes IS NULL)
        AND (scheduled_code IS NULL) = (scheduled_monthly_price IS NULL)
        AND (scheduled_code IS NULL) = (scheduled_at IS NULL)),
    CHECK ((paused_at IS NULL) = (pause_reason IS NULL))
);

-- A Bucket name is taken in its Region until the Storage is fully deleted,
-- whichever Team holds it.
CREATE UNIQUE INDEX storages_bucket_name_idx ON storages (region, bucket_name) WHERE status <> 'deleted';
CREATE INDEX storages_project_id_idx ON storages (project_id);
CREATE INDEX storages_team_id_idx ON storages (team_id);
CREATE INDEX storages_status_idx ON storages (status) WHERE status <> 'deleted';

-- The secret key is never kept: it leaves the engine once, to the member who
-- made the Access key.
CREATE TABLE access_keys (
    id            UUID PRIMARY KEY,
    storage_id    UUID NOT NULL REFERENCES storages (id),
    name          TEXT NOT NULL,
    access_key_id TEXT NOT NULL,
    permission    TEXT NOT NULL CHECK (permission IN ('read', 'read_write')),
    created_by    UUID NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL
);

CREATE INDEX access_keys_storage_id_idx ON access_keys (storage_id);
