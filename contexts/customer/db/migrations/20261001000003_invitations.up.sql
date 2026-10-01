CREATE TYPE invitation_status AS ENUM ('pending', 'accepted', 'declined', 'revoked', 'expired');

CREATE TABLE invitations (
    id           UUID PRIMARY KEY DEFAULT uuidv7(),
    team_id      UUID NOT NULL REFERENCES teams (id),
    email        TEXT NOT NULL,
    role         team_role NOT NULL,
    invited_by   UUID NOT NULL REFERENCES users (id),
    token_hash   TEXT NOT NULL,
    expires_at   TIMESTAMPTZ NOT NULL,
    status       invitation_status NOT NULL DEFAULT 'pending',
    accepted_by  UUID REFERENCES users (id),
    responded_at TIMESTAMPTZ,
    last_sent_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX invitations_token_hash_unique ON invitations (token_hash);
-- One open offer per address per Team; answered and expired ones may pile up.
CREATE UNIQUE INDEX invitations_pending_unique ON invitations (team_id, email) WHERE status = 'pending';
CREATE INDEX invitations_email_idx ON invitations (email);
CREATE INDEX invitations_team_id_idx ON invitations (team_id);

-- A sign-up that came from an Invitation remembers it until email
-- verification completes it.
ALTER TABLE users ADD COLUMN pending_invitation_id UUID REFERENCES invitations (id);
