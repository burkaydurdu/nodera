-- Billing no longer addresses emails: the notification context decides who is
-- told and keeps the contact details itself (docs/adr/0008).
ALTER TABLE team_members DROP COLUMN email, DROP COLUMN full_name, DROP COLUMN locale;
