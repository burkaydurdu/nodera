ALTER TABLE users DROP COLUMN IF EXISTS pending_invitation_id;
DROP TABLE IF EXISTS invitations;
DROP TYPE IF EXISTS invitation_status;
