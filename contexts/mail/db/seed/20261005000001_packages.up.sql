-- Launch prices in USD per month, excluding taxes (docs/specs/2026-10-05-phase-5-mail.md).
INSERT INTO packages (code, monthly_emails, monthly_price) VALUES
    ('5k', 5000, 5.00),
    ('10k', 10000, 9.00),
    ('25k', 25000, 19.00),
    ('50k', 50000, 29.00),
    ('100k', 100000, 49.00)
ON CONFLICT (code) DO NOTHING;
