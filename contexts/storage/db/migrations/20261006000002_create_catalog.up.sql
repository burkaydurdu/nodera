-- The Regions Nodera keeps Storage in, named by the droplet catalog's codes.
CREATE TABLE regions (
    code       TEXT PRIMARY KEY,
    city       TEXT NOT NULL,
    country    TEXT NOT NULL,
    active     BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The Package catalog the seed fills and the Admin side will edit.
CREATE TABLE packages (
    code          TEXT PRIMARY KEY,
    size_bytes    BIGINT NOT NULL CHECK (size_bytes > 0),
    monthly_price NUMERIC(12, 2) NOT NULL CHECK (monthly_price >= 0),
    active        BOOLEAN NOT NULL DEFAULT true,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
