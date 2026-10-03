-- A person's name and Locale change apart from their memberships; the time of
-- the last profile change keeps an older membership event from writing an
-- older name back.
ALTER TABLE team_members ADD COLUMN profile_updated_at TIMESTAMPTZ NULL;
