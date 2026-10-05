-- One row per Package a Project held, fed by the mail context's
-- MailPackageStarted and MailPackageStopped events and keyed the way mail
-- names an interval: the Project and the instant it started. What a Period
-- owes is derived on read. A row without a price is a stop that arrived
-- before its start; it counts for nothing until the start fills it in.
CREATE TABLE mail_package_intervals (
    project_id     UUID NOT NULL,
    started_at     TIMESTAMPTZ NOT NULL,
    team_id        UUID NOT NULL,
    project_name   TEXT NOT NULL DEFAULT '',
    package_code   TEXT NOT NULL DEFAULT '',
    monthly_emails INTEGER NULL CHECK (monthly_emails > 0),
    monthly_price  NUMERIC(12, 2) NULL CHECK (monthly_price >= 0),
    stopped_at     TIMESTAMPTZ NULL CHECK (stopped_at >= started_at),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, started_at),
    CHECK ((monthly_price IS NULL) = (monthly_emails IS NULL))
);

CREATE INDEX mail_package_intervals_team_id_idx ON mail_package_intervals (team_id) WHERE monthly_price IS NOT NULL;
