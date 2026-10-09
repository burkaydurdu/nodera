-- The droplet catalog as this context last heard it (docs/adr/0014):
-- synced_at is the time of the event that shaped the row, so an older copy
-- arriving later changes nothing.
CREATE TABLE catalog_regions (
    code      TEXT PRIMARY KEY,
    city      TEXT NOT NULL,
    country   TEXT NOT NULL,
    enabled   BOOLEAN NOT NULL,
    synced_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE catalog_plans (
    code          TEXT NOT NULL,
    region_code   TEXT NOT NULL,
    cpu_count     INTEGER NOT NULL,
    ram_gb        INTEGER NOT NULL,
    hourly_price  NUMERIC(12, 4) NOT NULL,
    monthly_price NUMERIC(12, 2) NOT NULL,
    sold_out      BOOLEAN NOT NULL,
    enabled       BOOLEAN NOT NULL,
    synced_at     TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (code, region_code)
);

-- The Kubernetes versions Nodera offers; closing one is an UPDATE, never a
-- deploy (docs/adr/0013).
CREATE TABLE kubernetes_versions (
    version    TEXT PRIMARY KEY,
    offered    BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
