DROP INDEX IF EXISTS clusters_endpoint_port_idx;
ALTER TABLE clusters DROP COLUMN IF EXISTS endpoint_port;
