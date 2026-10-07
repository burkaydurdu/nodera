-- What a Storage held at the last measurement of each UTC day.
CREATE TABLE storage_sizes (
    storage_id   UUID NOT NULL,
    day          DATE NOT NULL,
    used_bytes   BIGINT NOT NULL,
    object_count BIGINT NOT NULL,
    PRIMARY KEY (storage_id, day)
);

-- Traffic as the engine's usage log reports it, one row per hour so that
-- reading an hour again replaces it instead of counting it twice. Nothing is
-- billed from it (docs/adr/0012).
CREATE TABLE storage_traffic (
    storage_id     UUID NOT NULL,
    hour           TIMESTAMPTZ NOT NULL,
    bytes_sent     BIGINT NOT NULL,
    bytes_received BIGINT NOT NULL,
    requests       BIGINT NOT NULL,
    PRIMARY KEY (storage_id, hour)
);
