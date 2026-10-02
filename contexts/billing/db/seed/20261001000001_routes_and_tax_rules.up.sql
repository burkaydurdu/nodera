-- Routing (ADR-0006): both rows point at Craftgate until a non-Turkish entity
-- exists; the default row then becomes stripe/USD by SQL, not by deploy.
INSERT INTO payment_routes (country, provider, currency) VALUES
    ('TR', 'craftgate', 'TRY'),
    ('*', 'craftgate', 'USD')
ON CONFLICT (country) DO NOTHING;

-- First two Tax rules, to be confirmed with the accountant: 20% KDV for
-- Turkish Teams, the export-service exemption for everyone else.
INSERT INTO tax_rules (country, account_type, rate_bps, label_key, valid_from) VALUES
    ('TR', '*', 2000, 'tax.kdv_20', '2026-01-01T00:00:00Z'),
    ('*', '*', 0, 'tax.export_service_exemption', '2026-01-01T00:00:00Z');
