-- The central bank's USD buying rate per day (ADR-0006). Money is numeric,
-- never a float.
CREATE TABLE exchange_rates (
    rate_date  DATE PRIMARY KEY,
    usd_buying NUMERIC(18, 6) NOT NULL CHECK (usd_buying > 0),
    source     TEXT NOT NULL,
    fetched_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
