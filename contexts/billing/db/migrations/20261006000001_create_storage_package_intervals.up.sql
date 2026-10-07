-- One row per Package a Storage held, fed by the storage context's
-- StoragePackageStarted and StoragePackageStopped events and keyed the way
-- storage names an interval: the Storage and the instant it started. What a
-- Period owes is derived on read. A row without a price is a stop that
-- arrived before its start; it counts for nothing until the start fills it in.
CREATE TABLE storage_package_intervals (
    storage_id    UUID NOT NULL,
    started_at    TIMESTAMPTZ NOT NULL,
    team_id       UUID NOT NULL,
    project_id    UUID NULL,
    project_name  TEXT NOT NULL DEFAULT '',
    bucket_name   TEXT NOT NULL DEFAULT '',
    region        TEXT NOT NULL DEFAULT '',
    package_code  TEXT NOT NULL DEFAULT '',
    size_bytes    BIGINT NULL CHECK (size_bytes > 0),
    monthly_price NUMERIC(12, 2) NULL CHECK (monthly_price >= 0),
    stopped_at    TIMESTAMPTZ NULL CHECK (stopped_at >= started_at),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (storage_id, started_at),
    CHECK ((monthly_price IS NULL) = (size_bytes IS NULL) AND (monthly_price IS NULL) = (project_id IS NULL))
);

CREATE INDEX storage_package_intervals_team_id_idx ON storage_package_intervals (team_id) WHERE monthly_price IS NOT NULL;
