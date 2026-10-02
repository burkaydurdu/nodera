-- The dev seed Team (cmd/customer seed) holds a payment method, so local
-- development can create Droplets without the billing service running.
INSERT INTO team_billing (team_id, status, changed_at)
VALUES ('019a0000-0000-7000-8000-000000000101', 'active', now())
ON CONFLICT (team_id) DO NOTHING;
