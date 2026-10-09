-- A Node Droplet is opened at the kubernetes context's request to be a Node of
-- one of its Clusters (docs/adr/0014). node_id is unique so a redelivered
-- request opens no second Droplet; cluster_id and node_pool are what the web
-- app shows beside it. Nothing of this reaches a Droplet a customer opened.
ALTER TABLE droplets ADD COLUMN node_id UUID NULL;
ALTER TABLE droplets ADD COLUMN cluster_id UUID NULL;
ALTER TABLE droplets ADD COLUMN node_pool TEXT NULL;

CREATE UNIQUE INDEX droplets_node_id_idx ON droplets (node_id) WHERE node_id IS NOT NULL;

ALTER TABLE droplets ADD CONSTRAINT droplets_node_fields_check
    CHECK ((node_id IS NULL) = (cluster_id IS NULL) AND (node_id IS NULL) = (node_pool IS NULL));
