-- A Storage line names the Storage and its Bucket name beside the Project and
-- the Package code a Mail line already carries. Each kind still carries its
-- own columns and none of another's, so a reader can trust the kind.
ALTER TABLE invoice_lines
    ADD COLUMN storage_id UUID NULL,
    ADD COLUMN bucket_name TEXT NULL,
    DROP CONSTRAINT invoice_lines_kind_check,
    DROP CONSTRAINT invoice_lines_droplet_columns,
    DROP CONSTRAINT invoice_lines_mail_columns;

ALTER TABLE invoice_lines
    ADD CONSTRAINT invoice_lines_kind_check CHECK (kind IN ('droplet', 'mail', 'storage')),
    ADD CONSTRAINT invoice_lines_droplet_columns CHECK (kind <> 'droplet' OR (
        droplet_id IS NOT NULL AND hostname IS NOT NULL AND plan_code IS NOT NULL AND hours IS NOT NULL
        AND hourly_price IS NOT NULL AND project_id IS NULL AND project_name IS NULL AND package_code IS NULL
        AND storage_id IS NULL AND bucket_name IS NULL)),
    ADD CONSTRAINT invoice_lines_mail_columns CHECK (kind <> 'mail' OR (
        project_id IS NOT NULL AND project_name IS NOT NULL AND package_code IS NOT NULL
        AND droplet_id IS NULL AND hostname IS NULL AND plan_code IS NULL AND hours IS NULL AND hourly_price IS NULL
        AND storage_id IS NULL AND bucket_name IS NULL)),
    ADD CONSTRAINT invoice_lines_storage_columns CHECK (kind <> 'storage' OR (
        storage_id IS NOT NULL AND bucket_name IS NOT NULL AND project_id IS NOT NULL AND project_name IS NOT NULL
        AND package_code IS NOT NULL
        AND droplet_id IS NULL AND hostname IS NULL AND plan_code IS NULL AND hours IS NULL AND hourly_price IS NULL));
