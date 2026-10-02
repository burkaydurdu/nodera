-- When the InvoiceIssued email went out; the sweep sends it for an Invoice
-- whose issue was committed but whose email never was. Invoices issued
-- before this column existed were announced at issue.
ALTER TABLE invoices ADD COLUMN notified_at TIMESTAMPTZ NULL;
UPDATE invoices SET notified_at = issued_at;

CREATE INDEX invoices_issued_idx ON invoices (issued_at) WHERE status = 'issued';
CREATE INDEX payments_idempotency_lookup_idx ON payments (invoice_id, status);
-- Confirmation links that lapse are failed by the sweep, which starts Dunning.
CREATE INDEX payments_confirmation_expiry_idx ON payments (confirm_expires_at) WHERE status = 'confirmation_required';
