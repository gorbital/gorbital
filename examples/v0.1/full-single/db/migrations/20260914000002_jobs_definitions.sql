-- Job definition overrides (ADR-0033). A row exists only for definitions an
-- operator has changed; a NULL column means the code default applies. Rows
-- are never deleted, so versions only ever increase.

-- +goose Up
CREATE TABLE jobs_definitions (
    name         text        NOT NULL,
    -- Reserved for per-organisation job configuration; NULL means platform-wide.
    org_id       text,
    enabled      boolean,
    schedule     text,
    timeout_ms   bigint      CHECK (timeout_ms > 0),
    max_attempts integer     CHECK (max_attempts > 0),
    queue        text        CHECK (queue <> ''),
    priority     smallint    CHECK (priority BETWEEN 1 AND 4),
    version      bigint      NOT NULL CHECK (version > 0),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    updated_by   text        NOT NULL
);

CREATE UNIQUE INDEX jobs_definitions_platform_name ON jobs_definitions (name) WHERE org_id IS NULL;
CREATE UNIQUE INDEX jobs_definitions_org_name ON jobs_definitions (org_id, name) WHERE org_id IS NOT NULL;

CREATE TABLE jobs_definition_history (
    id         bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name       text        NOT NULL,
    org_id     text,
    action     text        NOT NULL CHECK (action IN ('updated', 'reset')),
    -- Overridden fields before and after, as a JSON object; {} means defaults.
    old_config jsonb       NOT NULL,
    new_config jsonb       NOT NULL,
    version    bigint      NOT NULL,
    reason     text        NOT NULL DEFAULT '',
    actor_kind text        NOT NULL,
    actor_id   text        NOT NULL,
    request_id text        NOT NULL DEFAULT '',
    changed_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX jobs_definition_history_name_id ON jobs_definition_history (name, id DESC);
