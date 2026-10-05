-- A table of Droplet lines alone cannot hold Mail lines, so rolling back
-- drops them.
DELETE FROM invoice_lines WHERE kind = 'mail';

ALTER TABLE invoice_lines
    DROP CONSTRAINT IF EXISTS invoice_lines_mail_columns,
    DROP CONSTRAINT IF EXISTS invoice_lines_droplet_columns,
    DROP COLUMN IF EXISTS package_code,
    DROP COLUMN IF EXISTS project_name,
    DROP COLUMN IF EXISTS project_id,
    DROP COLUMN IF EXISTS kind,
    ALTER COLUMN droplet_id SET NOT NULL,
    ALTER COLUMN hostname SET NOT NULL,
    ALTER COLUMN plan_code SET NOT NULL,
    ALTER COLUMN hours SET NOT NULL,
    ALTER COLUMN hourly_price SET NOT NULL;
