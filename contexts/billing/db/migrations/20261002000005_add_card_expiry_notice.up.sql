-- The expiry warning goes out once per card.
ALTER TABLE payment_methods ADD COLUMN expiry_notified_at TIMESTAMPTZ NULL;
