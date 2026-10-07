-- A table of Droplet and Mail lines cannot hold Storage lines, so rolling
-- back drops them.
DELETE FROM invoice_lines WHERE kind = 'storage';

ALTER TABLE invoice_lines
    DROP CONSTRAINT IF EXISTS invoice_lines_storage_columns,
    DROP CONSTRAINT IF EXISTS invoice_lines_mail_columns,
    DROP CONSTRAINT IF EXISTS invoice_lines_droplet_columns,
    DROP CONSTRAINT IF EXISTS invoice_lines_kind_check,
    DROP COLUMN IF EXISTS bucket_name,
    DROP COLUMN IF EXISTS storage_id;

ALTER TABLE invoice_lines
    ADD CONSTRAINT invoice_lines_kind_check CHECK (kind IN ('droplet', 'mail')),
    ADD CONSTRAINT invoice_lines_droplet_columns CHECK (kind <> 'droplet' OR (
        droplet_id IS NOT NULL AND hostname IS NOT NULL AND plan_code IS NOT NULL AND hours IS NOT NULL
        AND hourly_price IS NOT NULL AND project_id IS NULL AND project_name IS NULL AND package_code IS NULL)),
    ADD CONSTRAINT invoice_lines_mail_columns CHECK (kind <> 'mail' OR (
        project_id IS NOT NULL AND project_name IS NOT NULL AND package_code IS NOT NULL
        AND droplet_id IS NULL AND hostname IS NULL AND plan_code IS NULL AND hours IS NULL AND hourly_price IS NULL));
