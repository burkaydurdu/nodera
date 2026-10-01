CREATE TABLE regions (
    id         UUID PRIMARY KEY DEFAULT uuidv7(),
    code       TEXT NOT NULL UNIQUE,
    city       TEXT NOT NULL,
    country    CHAR(2) NOT NULL,
    enabled    BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE images (
    id         UUID PRIMARY KEY DEFAULT uuidv7(),
    code       TEXT NOT NULL UNIQUE,
    name       TEXT NOT NULL,
    family     TEXT NOT NULL,
    version    TEXT NOT NULL,
    enabled    BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A Region and an Image map to one Provider reference per Provider; the
-- customer never sees the reference.
CREATE TABLE provider_regions (
    provider_id  UUID NOT NULL REFERENCES providers (id),
    region_id    UUID NOT NULL REFERENCES regions (id),
    provider_ref TEXT NOT NULL,
    PRIMARY KEY (provider_id, region_id),
    UNIQUE (provider_id, provider_ref)
);

CREATE TABLE provider_images (
    provider_id  UUID NOT NULL REFERENCES providers (id),
    image_id     UUID NOT NULL REFERENCES images (id),
    provider_ref TEXT NOT NULL,
    PRIMARY KEY (provider_id, image_id)
);

-- Offers are the Provider's listing as it reports it; the sync upserts them
-- and unmapped ones are kept but never sold.
CREATE TABLE offers (
    id                  UUID PRIMARY KEY DEFAULT uuidv7(),
    provider_id         UUID NOT NULL REFERENCES providers (id),
    provider_plan_ref   TEXT NOT NULL,
    provider_region_ref TEXT NOT NULL,
    hourly_price        NUMERIC(12, 4) NOT NULL CHECK (hourly_price >= 0),
    stock               INTEGER NOT NULL DEFAULT 0 CHECK (stock >= 0),
    hardware            JSONB NOT NULL DEFAULT '{}'::jsonb,
    synced_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider_id, provider_plan_ref, provider_region_ref)
);

CREATE TABLE plans (
    id                      UUID PRIMARY KEY DEFAULT uuidv7(),
    code                    TEXT NOT NULL,
    region_id               UUID NOT NULL REFERENCES regions (id),
    offer_id                UUID NOT NULL REFERENCES offers (id),
    cpu_count               INTEGER NOT NULL,
    cpu_description         TEXT NOT NULL,
    ram_gb                  INTEGER NOT NULL,
    disk_description        TEXT NOT NULL,
    bandwidth_description   TEXT NOT NULL,
    commission_bps_override INTEGER CHECK (commission_bps_override >= 0),
    hourly_price            NUMERIC(12, 4) NOT NULL CHECK (hourly_price >= 0),
    monthly_cap_hours       INTEGER NOT NULL DEFAULT 672,
    sold_out                BOOLEAN NOT NULL DEFAULT false,
    enabled                 BOOLEAN NOT NULL DEFAULT true,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (code, region_id),
    UNIQUE (offer_id)
);

CREATE INDEX plans_region_id_idx ON plans (region_id);
