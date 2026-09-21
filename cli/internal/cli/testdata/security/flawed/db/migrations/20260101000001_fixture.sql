-- The flawed fixture's schema: one mistake per rule, and nothing else.

-- +goose Up
CREATE TABLE fixture_sessions (
    id         text        PRIMARY KEY,
    user_id    text        NOT NULL,
    -- credential-stored-unhashed: the session token itself.
    token      text        NOT NULL UNIQUE,
    created_at timestamptz NOT NULL
);

CREATE TABLE fixture_users (
    id            text PRIMARY KEY,
    email         text NOT NULL,
    password_hash text
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
