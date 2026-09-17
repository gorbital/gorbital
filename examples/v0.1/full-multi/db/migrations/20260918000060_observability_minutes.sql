-- Request minutes (ADR-0064): each instance's HTTP requests per minute,
-- method and route, written every 15 seconds and read by /ops/observability.

-- +goose Up
CREATE TABLE observability_minutes (
    -- The minute's first instant.
    minute          timestamptz NOT NULL,
    -- The release tracker's instance ID of the instance that served them.
    instance_id     text        NOT NULL,
    -- A standard HTTP method or _OTHER, and the path of the route pattern
    -- registered in code ('' when no route matched). Never a requested path.
    method          text        NOT NULL,
    route           text        NOT NULL,
    requests        bigint      NOT NULL,
    -- 4xx and 5xx responses.
    client_errors   bigint      NOT NULL,
    server_errors   bigint      NOT NULL,
    duration_sum_us bigint      NOT NULL,
    duration_max_us bigint      NOT NULL,
    -- Request counts per latency bucket, by observability.BucketBounds; the
    -- last one counts requests slower than every bound. Changing the bounds
    -- needs a migration.
    buckets         bigint[]    NOT NULL,
    -- When the instance last wrote the row.
    updated_at      timestamptz NOT NULL,
    PRIMARY KEY (minute, instance_id, method, route)
);
