-- country → Payment provider and currency (ADR-0006). '*' is the default row.
-- Operators change it by SQL; the choice is copied onto every Payment method
-- at creation so the provider never changes under a saved card.
CREATE TABLE payment_routes (
    country  TEXT PRIMARY KEY,
    provider TEXT NOT NULL,
    currency CHAR(3) NOT NULL
);

-- A card saved for Nodera to charge: only the provider's references and what
-- a customer needs to tell cards apart. The card number never reaches Nodera.
CREATE TABLE payment_methods (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id               UUID NOT NULL,
    provider              TEXT NOT NULL,
    currency              CHAR(3) NOT NULL,
    provider_customer_ref TEXT NOT NULL,
    provider_card_ref     TEXT NOT NULL,
    brand                 TEXT NOT NULL DEFAULT '',
    last4                 CHAR(4) NOT NULL,
    exp_month             SMALLINT NOT NULL,
    exp_year              SMALLINT NOT NULL,
    is_default            BOOLEAN NOT NULL DEFAULT false,
    created_by            UUID NOT NULL,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at            TIMESTAMPTZ NULL
);

CREATE INDEX payment_methods_team_id_idx ON payment_methods (team_id) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX payment_methods_one_default_idx ON payment_methods (team_id) WHERE is_default AND deleted_at IS NULL;

-- One hosted card verification: started by Nodera, finished by the browser
-- coming back from the provider. The provider's own reference is what the
-- completion asks the provider about.
CREATE TABLE card_verifications (
    id                UUID PRIMARY KEY,
    team_id           UUID NOT NULL,
    provider          TEXT NOT NULL,
    currency          CHAR(3) NOT NULL,
    provider_ref      TEXT NOT NULL,
    status            TEXT NOT NULL CHECK (status IN ('pending', 'completed', 'failed')),
    payment_method_id UUID NULL REFERENCES payment_methods (id),
    failure_reason    TEXT NOT NULL DEFAULT '',
    created_by        UUID NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at      TIMESTAMPTZ NULL
);

CREATE INDEX card_verifications_team_id_idx ON card_verifications (team_id);
