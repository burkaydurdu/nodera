-- The launch Region is where the storage cluster stands (docs/adr/0012); the
-- code is the droplet catalog's.
INSERT INTO regions (code, city, country) VALUES
    ('vno1', 'Šiauliai', 'LT')
ON CONFLICT (code) DO NOTHING;

-- Provisional prices: the real ones follow the cluster's cost, which is not
-- known until the servers are quoted. 1 GB is 1,000,000,000 bytes.
INSERT INTO packages (code, size_bytes, monthly_price) VALUES
    ('10gb',    10000000000,  1.00),
    ('50gb',    50000000000,  3.00),
    ('250gb',  250000000000, 10.00),
    ('1tb',   1000000000000, 35.00)
ON CONFLICT (code) DO NOTHING;
