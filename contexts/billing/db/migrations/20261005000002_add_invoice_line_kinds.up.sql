-- Invoice lines are product-neutral: a Droplet line keeps the Droplet's
-- columns, a Mail line names the Project, its name and the Package code. Each
-- kind must carry its own columns and none of the other's, so a reader can
-- trust the kind. Every line issued before Mail is a Droplet line.
ALTER TABLE invoice_lines
    ADD COLUMN kind TEXT NOT NULL DEFAULT 'droplet' CHECK (kind IN ('droplet', 'mail')),
    ADD COLUMN project_id UUID NULL,
    ADD COLUMN project_name TEXT NULL,
    ADD COLUMN package_code TEXT NULL,
    ALTER COLUMN droplet_id DROP NOT NULL,
    ALTER COLUMN hostname DROP NOT NULL,
    ALTER COLUMN plan_code DROP NOT NULL,
    ALTER COLUMN hours DROP NOT NULL,
    ALTER COLUMN hourly_price DROP NOT NULL;

-- The default only labelled the existing rows; a new line always names its kind.
ALTER TABLE invoice_lines ALTER COLUMN kind DROP DEFAULT;

ALTER TABLE invoice_lines
    ADD CONSTRAINT invoice_lines_droplet_columns CHECK (kind <> 'droplet' OR (
        droplet_id IS NOT NULL AND hostname IS NOT NULL AND plan_code IS NOT NULL AND hours IS NOT NULL
        AND hourly_price IS NOT NULL AND project_id IS NULL AND project_name IS NULL AND package_code IS NULL)),
    ADD CONSTRAINT invoice_lines_mail_columns CHECK (kind <> 'mail' OR (
        project_id IS NOT NULL AND project_name IS NOT NULL AND package_code IS NOT NULL
        AND droplet_id IS NULL AND hostname IS NULL AND plan_code IS NULL AND hours IS NULL AND hourly_price IS NULL));
