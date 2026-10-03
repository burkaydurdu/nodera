-- One row per Notification and Recipient, kept for good. The id is a UUIDv7,
-- so the Inbox pages by id, newest first. source_key names what happened;
-- with the recipient key it makes a redelivered event a no-op. Secrets (a
-- code, a reset link) never reach params.
CREATE TABLE notifications (
    id         UUID PRIMARY KEY DEFAULT uuidv7(),
    source_key TEXT NOT NULL,
    recipient  TEXT NOT NULL,
    type       TEXT NOT NULL,
    team_id    UUID NULL,
    user_id    UUID NULL,
    email      TEXT NOT NULL DEFAULT '',
    full_name  TEXT NOT NULL DEFAULT '',
    locale     TEXT NOT NULL DEFAULT '',
    params     JSONB NOT NULL DEFAULT '{}'::jsonb,
    in_inbox   BOOLEAN NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    read_at    TIMESTAMPTZ NULL,
    UNIQUE (source_key, recipient)
);

CREATE INDEX notifications_inbox_idx ON notifications (user_id, id DESC) WHERE in_inbox;
CREATE INDEX notifications_unread_idx ON notifications (user_id) WHERE in_inbox AND read_at IS NULL;

-- One row per Channel a Notification goes out on. Workers never retry
-- (the shovel redelivers from the error queue), so a redelivery delivers only
-- the Channels still undelivered and never sends an email twice.
CREATE TABLE notification_deliveries (
    notification_id UUID NOT NULL REFERENCES notifications (id) ON DELETE CASCADE,
    channel         TEXT NOT NULL,
    delivered_at    TIMESTAMPTZ NULL,
    attempts        INT NOT NULL DEFAULT 0,
    last_error      TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (notification_id, channel)
);

CREATE INDEX notification_deliveries_pending_idx ON notification_deliveries (notification_id) WHERE delivered_at IS NULL;
