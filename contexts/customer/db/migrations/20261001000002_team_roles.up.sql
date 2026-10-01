-- Own file on purpose: a value added to an enum cannot be used in the same
-- transaction, and golang-migrate runs each file as one.
ALTER TYPE team_role ADD VALUE IF NOT EXISTS 'developer';
ALTER TYPE team_role ADD VALUE IF NOT EXISTS 'finance';
