-- The catalog the seed fills and the Admin side will edit.
CREATE TABLE packages (
    code           TEXT PRIMARY KEY,
    monthly_emails INTEGER NOT NULL CHECK (monthly_emails > 0),
    monthly_price  NUMERIC(12, 2) NOT NULL CHECK (monthly_price >= 0),
    active         BOOLEAN NOT NULL DEFAULT true,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A Project's Package copies the catalog's price when chosen, like a Droplet's
-- Locked price. A NULL code is a Project without a Package; scheduled_code is
-- the change the next Period starts with, 'none' for a cancellation, and a
-- smaller Package's terms are copied when it is scheduled too.
CREATE TABLE project_packages (
    project_id               UUID PRIMARY KEY,
    team_id                  UUID NOT NULL,
    code                     TEXT,
    monthly_emails           INTEGER,
    monthly_price            NUMERIC(12, 2),
    started_at               TIMESTAMPTZ,
    scheduled_code           TEXT,
    scheduled_monthly_emails INTEGER,
    scheduled_monthly_price  NUMERIC(12, 2),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((code IS NULL) = (monthly_emails IS NULL) AND (code IS NULL) = (monthly_price IS NULL) AND (code IS NULL) = (started_at IS NULL)),
    CHECK ((scheduled_code IS NULL OR scheduled_code = 'none') = (scheduled_monthly_emails IS NULL)
        AND (scheduled_monthly_emails IS NULL) = (scheduled_monthly_price IS NULL))
);

CREATE INDEX project_packages_team_id_idx ON project_packages (team_id);

-- One row per Project and calendar month (UTC).
CREATE TABLE mail_periods (
    project_id             UUID NOT NULL,
    period_start           TIMESTAMPTZ NOT NULL,
    allowance              INTEGER NOT NULL CHECK (allowance >= 0),
    used                   INTEGER NOT NULL DEFAULT 0 CHECK (used >= 0),
    near_limit_notified_at TIMESTAMPTZ,
    exhausted_notified_at  TIMESTAMPTZ,
    PRIMARY KEY (project_id, period_start)
);
