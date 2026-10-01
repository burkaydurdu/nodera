CREATE TYPE user_status AS ENUM ('pending_verification', 'active', 'suspended');
CREATE TYPE user_mfa_method AS ENUM ('none', 'email');

CREATE TABLE users (
    id                UUID PRIMARY KEY DEFAULT uuidv7(),
    email             TEXT NOT NULL,
    password          TEXT NOT NULL,
    full_name         TEXT NOT NULL,
    locale            TEXT NOT NULL CHECK (locale IN ('tr', 'en')),
    country           CHAR(2) NOT NULL,
    status            user_status NOT NULL DEFAULT 'pending_verification',
    mfa_method        user_mfa_method NOT NULL DEFAULT 'email',
    email_verified_at TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at        TIMESTAMPTZ
);

CREATE UNIQUE INDEX users_email_unique ON users (lower(email)) WHERE deleted_at IS NULL;
