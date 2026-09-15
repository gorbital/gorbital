-- Release tracking (ADR-0040): one row per instance start. A release is a
-- (version, commit) pair; an instance is running while it hasn't stopped and
-- its heartbeat is recent. Trackers delete instances last seen before the
-- retention.

-- +goose Up
CREATE TABLE release_instances (
    id           bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    -- Random per process start.
    instance_id  text        NOT NULL UNIQUE,
    version      text        NOT NULL,
    commit       text        NOT NULL DEFAULT '',
    -- NULL when the build recorded no time.
    build_time   timestamptz,
    modified     boolean     NOT NULL DEFAULT false,
    go_version   text        NOT NULL DEFAULT '',
    host         text        NOT NULL DEFAULT '',
    started_at   timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL,
    stopped_at   timestamptz
);

CREATE INDEX release_instances_release ON release_instances (version, commit);
CREATE INDEX release_instances_last_seen ON release_instances (last_seen_at);
