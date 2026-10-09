-- A Cluster's API server answers on a port of the Region's address that is its
-- alone, and its tunnel on the port after it (docs/adr/0015). Clusters opened
-- before take ports from the start of the default range, two by two.
ALTER TABLE clusters ADD COLUMN endpoint_port INTEGER;

UPDATE clusters
SET endpoint_port = 20000 + 2 * (numbered.n - 1)
FROM (SELECT id, row_number() OVER (PARTITION BY region ORDER BY created_at, id) AS n FROM clusters) AS numbered
WHERE clusters.id = numbered.id;

ALTER TABLE clusters ALTER COLUMN endpoint_port SET NOT NULL;

CREATE UNIQUE INDEX clusters_endpoint_port_idx ON clusters (region, endpoint_port) WHERE status <> 'deleted';
