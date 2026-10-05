-- The Daily limit, the Pause and the rate warning of a Project.
-- rates_reset_at is when an operator lifted the last Pause: the days before it
-- are not judged again.
CREATE TABLE project_limits (
    project_id      UUID PRIMARY KEY,
    team_id         UUID NOT NULL,
    daily_limit     INTEGER NOT NULL CHECK (daily_limit > 0),
    paused_at       TIMESTAMPTZ,
    pause_reason    TEXT CHECK (pause_reason IN ('bounce_rate', 'complaint_rate')),
    rates_warned_at TIMESTAMPTZ,
    rates_reset_at  TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One row per Project and UTC day: what it sent against the limit of that day
-- and the outcomes recorded that day. bounced counts permanent failures only.
CREATE TABLE mail_days (
    project_id   UUID NOT NULL,
    day          DATE NOT NULL,
    daily_limit  INTEGER NOT NULL,
    sent         INTEGER NOT NULL DEFAULT 0,
    delivered    INTEGER NOT NULL DEFAULT 0,
    bounced      INTEGER NOT NULL DEFAULT 0,
    complained   INTEGER NOT NULL DEFAULT 0,
    evaluated_at TIMESTAMPTZ,
    PRIMARY KEY (project_id, day)
);

CREATE INDEX mail_days_day_idx ON mail_days (day);
