ALTER TABLE droplets DROP CONSTRAINT IF EXISTS droplets_node_fields_check;
DROP INDEX IF EXISTS droplets_node_id_idx;
ALTER TABLE droplets DROP COLUMN IF EXISTS node_pool;
ALTER TABLE droplets DROP COLUMN IF EXISTS cluster_id;
ALTER TABLE droplets DROP COLUMN IF EXISTS node_id;
