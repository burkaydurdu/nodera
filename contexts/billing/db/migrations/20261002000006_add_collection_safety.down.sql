DROP INDEX IF EXISTS payments_confirmation_expiry_idx;
DROP INDEX IF EXISTS payments_idempotency_lookup_idx;
DROP INDEX IF EXISTS invoices_issued_idx;
ALTER TABLE invoices DROP COLUMN IF EXISTS notified_at;
