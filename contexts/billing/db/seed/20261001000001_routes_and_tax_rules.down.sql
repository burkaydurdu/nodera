DELETE FROM tax_rules WHERE label_key IN ('tax.kdv_20', 'tax.export_service_exemption');
DELETE FROM payment_routes WHERE country IN ('TR', '*');
