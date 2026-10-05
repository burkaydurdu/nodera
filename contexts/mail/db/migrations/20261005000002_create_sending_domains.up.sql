-- A Sending domain and its two DKIM selectors (docs/adr/0011). The token
-- makes every record the Team adds unique to this row, so another Project's
-- records never verify it.
CREATE TABLE sending_domains (
    id                 UUID PRIMARY KEY,
    project_id         UUID NOT NULL,
    team_id            UUID NOT NULL,
    name               TEXT NOT NULL,
    token              TEXT NOT NULL UNIQUE,
    status             TEXT NOT NULL CHECK (status IN ('pending', 'verified', 'unverified')),
    active_selector    TEXT NOT NULL DEFAULT 'a' CHECK (active_selector IN ('a', 'b')),
    return_path_valid  BOOLEAN NOT NULL DEFAULT false,
    dkim_a_valid       BOOLEAN NOT NULL DEFAULT false,
    dkim_b_valid       BOOLEAN NOT NULL DEFAULT false,
    dmarc_found        BOOLEAN NOT NULL DEFAULT false,
    last_checked_at    TIMESTAMPTZ,
    next_check_at      TIMESTAMPTZ NOT NULL,
    verified_at        TIMESTAMPTZ,
    -- A rotation publishes a fresh key under the idle selector and signs with
    -- it only once rotation_ready_at has passed.
    rotation_selector  TEXT CHECK (rotation_selector IN ('a', 'b')),
    rotation_ready_at  TIMESTAMPTZ,
    keys_rotated_at    TIMESTAMPTZ NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, name)
);

CREATE INDEX sending_domains_next_check_idx ON sending_domains (next_check_at);

-- private_key is the PKCS#8 key sealed with AES-256-GCM; public_key is the
-- base64 SubjectPublicKeyInfo the DKIM TXT record publishes.
CREATE TABLE dkim_keys (
    sending_domain_id UUID NOT NULL REFERENCES sending_domains (id) ON DELETE CASCADE,
    selector          TEXT NOT NULL CHECK (selector IN ('a', 'b')),
    private_key       BYTEA NOT NULL,
    public_key        TEXT NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (sending_domain_id, selector)
);
