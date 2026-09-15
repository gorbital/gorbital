-- Runtime settings (ADR-0031). Rows exist only for settings that have been
-- changed; a NULL value means the setting was reset to its default. Rows are
-- never deleted, so versions only ever increase.

-- +goose Up
CREATE TABLE settings_values (
    key        text        NOT NULL,
    -- Reserved for per-organisation settings; NULL means platform-wide.
    org_id     text,
    value      jsonb,
    version    bigint      NOT NULL CHECK (version > 0),
    updated_at timestamptz NOT NULL DEFAULT now(),
    updated_by text        NOT NULL
);

CREATE UNIQUE INDEX settings_values_platform_key ON settings_values (key) WHERE org_id IS NULL;
CREATE UNIQUE INDEX settings_values_org_key ON settings_values (org_id, key) WHERE org_id IS NOT NULL;

CREATE TABLE settings_history (
    id         bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    key        text        NOT NULL,
    org_id     text,
    old_value  jsonb,
    new_value  jsonb,
    version    bigint      NOT NULL,
    reason     text        NOT NULL DEFAULT '',
    actor_kind text        NOT NULL,
    actor_id   text        NOT NULL,
    request_id text        NOT NULL DEFAULT '',
    changed_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX settings_history_key_id ON settings_history (key, id DESC);
