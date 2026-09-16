-- Feature flags (ADR-0057). Rows exist only for flags an operator has
-- changed; a NULL state means the flag was reset to its declared default.
-- Rows are never deleted, so versions only ever increase.

-- +goose Up
CREATE TABLE flags_states (
    key        text        PRIMARY KEY,
    -- {"enabled", "default", "orgs": {"allow", "deny"}, "users": {"allow",
    -- "deny"}, "percentage"}; validated by the library on write and load.
    state      jsonb,
    version    bigint      NOT NULL CHECK (version > 0),
    updated_at timestamptz NOT NULL DEFAULT now(),
    updated_by text        NOT NULL
);

CREATE TABLE flags_history (
    id         bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    key        text        NOT NULL,
    old_state  jsonb,
    new_state  jsonb,
    version    bigint      NOT NULL,
    reason     text        NOT NULL,
    actor_kind text        NOT NULL,
    actor_id   text        NOT NULL,
    request_id text        NOT NULL DEFAULT '',
    changed_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX flags_history_key_id ON flags_history (key, id DESC);
