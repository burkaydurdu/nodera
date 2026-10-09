-- A Cluster (docs/adr/0013): its Control plane runs in the engine, its Nodes
-- are Droplets the droplet context opens at this context's request
-- (docs/adr/0014). The control_plane_* and prepared_at columns say what the
-- engine was last made to do; the jobs work on the rows where they lag.
CREATE TABLE clusters (
    id                       UUID PRIMARY KEY,
    project_id               UUID NOT NULL,
    team_id                  UUID NOT NULL,
    region                   TEXT NOT NULL,
    name                     TEXT NOT NULL,
    version                  TEXT NOT NULL,
    status                   TEXT NOT NULL CHECK (status IN ('provisioning', 'running', 'degraded', 'deleting', 'deleted', 'failed')),
    endpoint                 TEXT NOT NULL,
    team_suspended           BOOLEAN NOT NULL DEFAULT false,
    failure_code             TEXT,
    failure_reason           TEXT,
    control_plane_created_at TIMESTAMPTZ,
    control_plane_ready_at   TIMESTAMPTZ,
    prepared_at              TIMESTAMPTZ,
    control_plane_deleted_at TIMESTAMPTZ,
    created_by               UUID NOT NULL,
    created_at               TIMESTAMPTZ NOT NULL,
    updated_at               TIMESTAMPTZ NOT NULL,
    deleted_at               TIMESTAMPTZ,
    CHECK ((failure_code IS NULL) = (failure_reason IS NULL))
);

-- A name is taken in its Project until the Cluster is fully deleted.
CREATE UNIQUE INDEX clusters_name_idx ON clusters (project_id, name) WHERE status <> 'deleted';
CREATE INDEX clusters_project_id_idx ON clusters (project_id);
CREATE INDEX clusters_team_id_idx ON clusters (team_id);
CREATE INDEX clusters_status_idx ON clusters (status) WHERE status <> 'deleted';

CREATE TABLE node_pools (
    id         UUID PRIMARY KEY,
    cluster_id UUID NOT NULL REFERENCES clusters (id),
    name       TEXT NOT NULL,
    plan_code  TEXT NOT NULL,
    count      INTEGER NOT NULL CHECK (count >= 1),
    labels     JSONB NOT NULL DEFAULT '{}'::jsonb,
    taints     JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    deleted_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX node_pools_name_idx ON node_pools (cluster_id, name) WHERE deleted_at IS NULL;
CREATE INDEX node_pools_cluster_id_idx ON node_pools (cluster_id);

-- requested_at is when the request went to the droplet context; null while the
-- Control plane cannot be joined yet. The droplet_* columns are what the
-- droplet context last said about the Node's Droplet.
CREATE TABLE nodes (
    id                UUID PRIMARY KEY,
    cluster_id        UUID NOT NULL REFERENCES clusters (id),
    pool_id           UUID NOT NULL REFERENCES node_pools (id),
    hostname          TEXT NOT NULL,
    status            TEXT NOT NULL CHECK (status IN ('requested', 'joining', 'ready', 'not_ready', 'releasing', 'gone', 'failed')),
    droplet_id        UUID,
    droplet_status    TEXT NOT NULL DEFAULT '',
    ipv4              TEXT,
    failure_code      TEXT,
    failure_reason    TEXT,
    requested_at      TIMESTAMPTZ,
    droplet_active_at TIMESTAMPTZ,
    ready_at          TIMESTAMPTZ,
    release_asked_at  TIMESTAMPTZ,
    released_at       TIMESTAMPTZ,
    gone_at           TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL,
    updated_at        TIMESTAMPTZ NOT NULL,
    CHECK ((failure_code IS NULL) = (failure_reason IS NULL))
);

CREATE INDEX nodes_cluster_id_idx ON nodes (cluster_id);
CREATE INDEX nodes_pool_id_idx ON nodes (pool_id);
CREATE INDEX nodes_status_idx ON nodes (status) WHERE status <> 'gone';
