-- Authentication (ADR-0024, ADR-0038): users, sessions, one-time email codes
-- and platform role assignments. Tokens and codes are stored only as hashes.

-- +goose Up
CREATE TABLE auth_users (
    id                  text        PRIMARY KEY,
    email               text        NOT NULL,
    -- Lowercased email; unique among accounts that aren't deleted.
    email_normalized    text        NOT NULL,
    -- NULL for accounts without a password (reserved for social sign-in).
    password_hash       text,
    email_verified_at   timestamptz,
    password_changed_at timestamptz,
    created_at          timestamptz NOT NULL,
    updated_at          timestamptz NOT NULL,
    deleted_at          timestamptz
);

CREATE UNIQUE INDEX auth_users_email ON auth_users (email_normalized) WHERE deleted_at IS NULL;
CREATE INDEX auth_users_deleted ON auth_users (deleted_at) WHERE deleted_at IS NOT NULL;

CREATE TABLE auth_sessions (
    id                  text        PRIMARY KEY,
    user_id             text        NOT NULL REFERENCES auth_users (id) ON DELETE CASCADE,
    -- SHA-256 of the session token; the token itself is never stored.
    token_hash          bytea       NOT NULL UNIQUE,
    -- Reserved for organisation-scoped sessions (ADR-0023).
    org_id              text,
    created_at          timestamptz NOT NULL,
    last_seen_at        timestamptz NOT NULL,
    idle_expires_at     timestamptz NOT NULL,
    absolute_expires_at timestamptz NOT NULL,
    revoked_at          timestamptz,
    revoked_reason      text        NOT NULL DEFAULT '',
    ip                  inet,
    user_agent          text        NOT NULL DEFAULT ''
);

CREATE INDEX auth_sessions_user ON auth_sessions (user_id, created_at DESC);
CREATE INDEX auth_sessions_expiry ON auth_sessions (absolute_expires_at);

CREATE TABLE auth_codes (
    id           text        PRIMARY KEY,
    user_id      text        NOT NULL REFERENCES auth_users (id) ON DELETE CASCADE,
    purpose      text        NOT NULL CHECK (purpose IN ('verify_email', 'reset_password')),
    -- SHA-256 of the code ID and the code.
    code_hash    bytea       NOT NULL,
    attempts     integer     NOT NULL DEFAULT 0,
    max_attempts integer     NOT NULL CHECK (max_attempts > 0),
    expires_at   timestamptz NOT NULL,
    consumed_at  timestamptz,
    created_at   timestamptz NOT NULL
);

CREATE INDEX auth_codes_user_purpose ON auth_codes (user_id, purpose, created_at DESC);

CREATE TABLE auth_user_roles (
    user_id    text        NOT NULL REFERENCES auth_users (id) ON DELETE CASCADE,
    role       text        NOT NULL,
    -- NULL for platform roles; reserved for organisation roles (ADR-0023).
    org_id     text,
    granted_at timestamptz NOT NULL,
    granted_by text        NOT NULL
);

CREATE UNIQUE INDEX auth_user_roles_platform ON auth_user_roles (user_id, role) WHERE org_id IS NULL;
CREATE UNIQUE INDEX auth_user_roles_org ON auth_user_roles (org_id, user_id, role) WHERE org_id IS NOT NULL;
