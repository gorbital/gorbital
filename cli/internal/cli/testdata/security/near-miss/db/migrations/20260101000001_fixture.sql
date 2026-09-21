-- The near-miss fixture's schema: the same tables, written the way that
-- must never be reported.

-- +goose Up
CREATE TABLE fixture_sessions (
    id         text        PRIMARY KEY,
    user_id    text        NOT NULL,
    -- SHA-256 of the session token; the token itself is never stored.
    token_hash bytea       NOT NULL UNIQUE,
    created_at timestamptz NOT NULL
);

CREATE TABLE fixture_users (
    id            text PRIMARY KEY,
    email         text NOT NULL,
    password_hash text
);

CREATE TABLE fixture_identities (
    id                       text  PRIMARY KEY,
    user_id                  text  NOT NULL REFERENCES fixture_users (id),
    -- Kept to revoke it, encrypted; the name says so, so it is not reported.
    refresh_token_ciphertext bytea,
    refresh_key_id           text,
    api_key_prefix           text
);

CREATE TABLE fixture_tenants (
    id         text        PRIMARY KEY,
    name       text        NOT NULL,
    deleted_at timestamptz
);

CREATE TABLE fixture_members (
    tenant_id text NOT NULL REFERENCES fixture_tenants (id) ON DELETE CASCADE,
    user_id   text NOT NULL REFERENCES fixture_users (id) ON DELETE CASCADE,
    role      text NOT NULL,
    PRIMARY KEY (tenant_id, user_id)
);
