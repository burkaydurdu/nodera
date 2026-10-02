-- Operator-managed Tax rules by billing country and account type; '*' matches
-- any. Lookup picks the most specific rule valid at the issue date. The label
-- is a translation key so the Invoice PDF renders it in the Team's Locale.
CREATE TABLE tax_rules (
    id           BIGSERIAL PRIMARY KEY,
    country      TEXT NOT NULL,
    account_type TEXT NOT NULL,
    rate_bps     INTEGER NOT NULL CHECK (rate_bps >= 0),
    label_key    TEXT NOT NULL,
    valid_from   TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX tax_rules_lookup_idx ON tax_rules (country, account_type, valid_from DESC);
