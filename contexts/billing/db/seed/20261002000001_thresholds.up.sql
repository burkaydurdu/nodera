-- $50 during a Team's first three months, $500 afterwards (spec: Threshold).
INSERT INTO thresholds (kind, amount) VALUES
    ('new', 50.00),
    ('established', 500.00)
ON CONFLICT (kind) DO NOTHING;
