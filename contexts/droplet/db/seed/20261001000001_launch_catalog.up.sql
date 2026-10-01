-- Launch catalog: the first Provider, seven Regions, three Images and a first
-- set of Plans. Every Provider reference and USD hourly price below was read
-- from the live Cherry Servers API on 2026-10-01 (GET /v1/regions,
-- GET /v1/plans?currency=USD, GET /v1/plans/{plan}/images); the first catalog
-- sync refreshes the prices and stock.

INSERT INTO providers (id, code, name, enabled, default_commission_bps)
VALUES ('019b0000-0000-7000-8000-000000000001', 'cherry', 'Cherry Servers', true, 2000);

-- Regions. Cherry's Lithuanian site is in Šiauliai and is sold under the city
-- it serves; Chicago is in the launch set because it is where Cherry keeps most
-- of its hourly stock.
INSERT INTO regions (id, code, city, country) VALUES
    ('019b0000-0000-7000-8000-000000000101', 'fra1', 'Frankfurt', 'DE'),
    ('019b0000-0000-7000-8000-000000000102', 'ams1', 'Amsterdam', 'NL'),
    ('019b0000-0000-7000-8000-000000000103', 'vno1', 'Šiauliai',  'LT'),
    ('019b0000-0000-7000-8000-000000000104', 'sto1', 'Stockholm', 'SE'),
    ('019b0000-0000-7000-8000-000000000105', 'sgp1', 'Singapore', 'SG'),
    ('019b0000-0000-7000-8000-000000000106', 'tyo1', 'Tokyo',     'JP'),
    ('019b0000-0000-7000-8000-000000000107', 'chi1', 'Chicago',   'US');

INSERT INTO provider_regions (provider_id, region_id, provider_ref) VALUES
    ('019b0000-0000-7000-8000-000000000001', '019b0000-0000-7000-8000-000000000101', 'DE-Frankfurt'),
    ('019b0000-0000-7000-8000-000000000001', '019b0000-0000-7000-8000-000000000102', 'NL-Amsterdam'),
    ('019b0000-0000-7000-8000-000000000001', '019b0000-0000-7000-8000-000000000103', 'LT-Siauliai'),
    ('019b0000-0000-7000-8000-000000000001', '019b0000-0000-7000-8000-000000000104', 'SE-Stockholm'),
    ('019b0000-0000-7000-8000-000000000001', '019b0000-0000-7000-8000-000000000105', 'SG-Singapore'),
    ('019b0000-0000-7000-8000-000000000001', '019b0000-0000-7000-8000-000000000106', 'JP-Tokyo'),
    ('019b0000-0000-7000-8000-000000000001', '019b0000-0000-7000-8000-000000000107', 'US-Chicago');

INSERT INTO images (id, code, name, family, version) VALUES
    ('019b0000-0000-7000-8000-000000000201', 'ubuntu-24-04', 'Ubuntu 24.04 LTS', 'ubuntu', '24.04'),
    ('019b0000-0000-7000-8000-000000000202', 'ubuntu-22-04', 'Ubuntu 22.04 LTS', 'ubuntu', '22.04'),
    ('019b0000-0000-7000-8000-000000000203', 'debian-12',    'Debian 12',        'debian', '12');

-- Cherry names Ubuntu 22.04 without the architecture suffix the others carry.
INSERT INTO provider_images (provider_id, image_id, provider_ref) VALUES
    ('019b0000-0000-7000-8000-000000000001', '019b0000-0000-7000-8000-000000000201', 'ubuntu_24_04_64bit'),
    ('019b0000-0000-7000-8000-000000000001', '019b0000-0000-7000-8000-000000000202', 'ubuntu_22_04'),
    ('019b0000-0000-7000-8000-000000000001', '019b0000-0000-7000-8000-000000000203', 'debian_12_64bit');

-- Offers as the live API answered on 2026-10-01 (hourly USD, stock at that
-- moment); the first sync replaces both. Only e5-1620v4 in Šiauliai and
-- amd-epyc-9124p in Chicago had stock that day, so those are the Plans a first
-- Droplet can actually be created from.
INSERT INTO offers (id, provider_id, provider_plan_ref, provider_region_ref, hourly_price, stock, hardware) VALUES
    ('019b0000-0000-7000-8000-000000000301', '019b0000-0000-7000-8000-000000000001', 'e3-1240v3',       'LT-Siauliai',  0.0983, 0,   '{"cpu":"Intel Xeon E3-1240 v3","ram":"16GB","disk":"250GB SSD"}'),
    ('019b0000-0000-7000-8000-000000000302', '019b0000-0000-7000-8000-000000000001', 'e5-1620v4',       'LT-Siauliai',  0.2305, 118, '{"cpu":"Intel Xeon E5-1620 v4","ram":"32GB","disk":"2x 250GB SSD"}'),
    ('019b0000-0000-7000-8000-000000000303', '019b0000-0000-7000-8000-000000000001', 'e5-1620v4',       'NL-Amsterdam', 0.2305, 0,   '{"cpu":"Intel Xeon E5-1620 v4","ram":"32GB","disk":"2x 250GB SSD"}'),
    ('019b0000-0000-7000-8000-000000000304', '019b0000-0000-7000-8000-000000000001', 'amd-ryzen-7700x', 'LT-Siauliai',  0.3182, 0,   '{"cpu":"AMD Ryzen 7 7700X","ram":"64GB","disk":"2x 1TB NVMe"}'),
    ('019b0000-0000-7000-8000-000000000305', '019b0000-0000-7000-8000-000000000001', 'amd-ryzen-7700x', 'NL-Amsterdam', 0.3182, 0,   '{"cpu":"AMD Ryzen 7 7700X","ram":"64GB","disk":"2x 1TB NVMe"}'),
    ('019b0000-0000-7000-8000-000000000306', '019b0000-0000-7000-8000-000000000001', 'amd-ryzen-7700x', 'SE-Stockholm', 0.3182, 0,   '{"cpu":"AMD Ryzen 7 7700X","ram":"64GB","disk":"2x 1TB NVMe"}'),
    ('019b0000-0000-7000-8000-000000000307', '019b0000-0000-7000-8000-000000000001', 'amd-ryzen-7700x', 'DE-Frankfurt', 0.3182, 0,   '{"cpu":"AMD Ryzen 7 7700X","ram":"64GB","disk":"2x 1TB NVMe"}'),
    ('019b0000-0000-7000-8000-000000000308', '019b0000-0000-7000-8000-000000000001', 'amd-epyc-9124p',  'NL-Amsterdam', 0.7137, 0,   '{"cpu":"AMD EPYC 9124P","ram":"64GB","disk":"2x 1TB NVMe"}'),
    ('019b0000-0000-7000-8000-000000000309', '019b0000-0000-7000-8000-000000000001', 'amd-epyc-9124p',  'SG-Singapore', 0.7137, 0,   '{"cpu":"AMD EPYC 9124P","ram":"64GB","disk":"2x 1TB NVMe"}'),
    ('019b0000-0000-7000-8000-000000000310', '019b0000-0000-7000-8000-000000000001', 'amd-epyc-9124p',  'US-Chicago',   0.7137, 20,  '{"cpu":"AMD EPYC 9124P","ram":"64GB","disk":"2x 1TB NVMe"}');

-- Plan prices = Offer price × (1 + commission) rounded up to 4 decimals; the
-- sync recomputes them. c8-64 in fra1 carries a 1500 bps override as the
-- headline Plan the spec allows for.
INSERT INTO plans (id, code, region_id, offer_id, cpu_count, cpu_description, ram_gb, disk_description, bandwidth_description, commission_bps_override, hourly_price, sold_out) VALUES
    ('019b0000-0000-7000-8000-000000000401', 'c4-16',  '019b0000-0000-7000-8000-000000000103', '019b0000-0000-7000-8000-000000000301', 4,  '4 cores, 3.4 GHz',  16, '250 GB SSD',    '30 TB / month',  NULL, 0.1180, true),
    ('019b0000-0000-7000-8000-000000000402', 'c4-32',  '019b0000-0000-7000-8000-000000000103', '019b0000-0000-7000-8000-000000000302', 4,  '4 cores, 3.5 GHz',  32, '2x 250 GB SSD', '30 TB / month',  NULL, 0.2766, false),
    ('019b0000-0000-7000-8000-000000000403', 'c4-32',  '019b0000-0000-7000-8000-000000000102', '019b0000-0000-7000-8000-000000000303', 4,  '4 cores, 3.5 GHz',  32, '2x 250 GB SSD', '30 TB / month',  NULL, 0.2766, true),
    ('019b0000-0000-7000-8000-000000000404', 'c8-64',  '019b0000-0000-7000-8000-000000000103', '019b0000-0000-7000-8000-000000000304', 8,  '8 cores, 4.5 GHz',  64, '2x 1 TB NVMe',  '100 TB / month', NULL, 0.3819, true),
    ('019b0000-0000-7000-8000-000000000405', 'c8-64',  '019b0000-0000-7000-8000-000000000102', '019b0000-0000-7000-8000-000000000305', 8,  '8 cores, 4.5 GHz',  64, '2x 1 TB NVMe',  '100 TB / month', NULL, 0.3819, true),
    ('019b0000-0000-7000-8000-000000000406', 'c8-64',  '019b0000-0000-7000-8000-000000000104', '019b0000-0000-7000-8000-000000000306', 8,  '8 cores, 4.5 GHz',  64, '2x 1 TB NVMe',  '100 TB / month', NULL, 0.3819, true),
    ('019b0000-0000-7000-8000-000000000407', 'c8-64',  '019b0000-0000-7000-8000-000000000101', '019b0000-0000-7000-8000-000000000307', 8,  '8 cores, 4.5 GHz',  64, '2x 1 TB NVMe',  '100 TB / month', 1500, 0.3660, true),
    ('019b0000-0000-7000-8000-000000000408', 'c16-64', '019b0000-0000-7000-8000-000000000102', '019b0000-0000-7000-8000-000000000308', 16, '16 cores, 3.0 GHz', 64, '2x 1 TB NVMe',  '100 TB / month', NULL, 0.8565, true),
    ('019b0000-0000-7000-8000-000000000409', 'c16-64', '019b0000-0000-7000-8000-000000000105', '019b0000-0000-7000-8000-000000000309', 16, '16 cores, 3.0 GHz', 64, '2x 1 TB NVMe',  '100 TB / month', NULL, 0.8565, true),
    ('019b0000-0000-7000-8000-000000000410', 'c16-64', '019b0000-0000-7000-8000-000000000107', '019b0000-0000-7000-8000-000000000310', 16, '16 cores, 3.0 GHz', 64, '2x 1 TB NVMe',  '100 TB / month', NULL, 0.8565, false);
