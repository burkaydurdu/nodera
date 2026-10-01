CREATE TABLE providers (
    id                     UUID PRIMARY KEY DEFAULT uuidv7(),
    code                   TEXT NOT NULL UNIQUE,
    name                   TEXT NOT NULL,
    enabled                BOOLEAN NOT NULL DEFAULT true,
    default_commission_bps INTEGER NOT NULL CHECK (default_commission_bps >= 0),
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);
