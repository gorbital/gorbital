-- API keys and service accounts (ADR-0058): non-human principals with roles,
-- and keys for them and for users. Keys are stored only as SHA-256 hashes.

-- +goose Up
CREATE TABLE auth_service_accounts (
    id          text        PRIMARY KEY,
    -- NULL for a platform service account; otherwise the organisation it
    -- belongs to and can act in (multi-tenant apps).
    org_id      text,
    name        text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    description text        NOT NULL DEFAULT '' CHECK (char_length(description) <= 500),
    -- Platform roles, or the one organisation role; never a role that
    -- requires two-factor authentication.
    roles       text[]      NOT NULL DEFAULT '{}',
    created_by  text        NOT NULL,
    created_at  timestamptz NOT NULL,
    updated_at  timestamptz NOT NULL,
    disabled_at timestamptz
);

CREATE INDEX auth_service_accounts_org ON auth_service_accounts (org_id, created_at);

CREATE TABLE auth_api_keys (
    id                 text        PRIMARY KEY,
    -- The middle part of the key, used to find it; not a secret.
    lookup_id          text        NOT NULL UNIQUE,
    -- SHA-256 of the whole key; the key itself is never stored.
    secret_hash        bytea       NOT NULL,
    -- Exactly one owner: a user or a service account.
    user_id            text        REFERENCES auth_users (id) ON DELETE CASCADE,
    service_account_id text        REFERENCES auth_service_accounts (id) ON DELETE CASCADE,
    name               text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    -- Permission names the key is limited to; empty: its owner's.
    scopes             text[]      NOT NULL DEFAULT '{}',
    expires_at         timestamptz NOT NULL,
    created_by         text        NOT NULL,
    created_at         timestamptz NOT NULL,
    last_used_at       timestamptz,
    revoked_at         timestamptz,
    revoked_reason     text        NOT NULL DEFAULT '',
    -- When cleanup recorded the key's expiry in the audit log.
    expiry_recorded_at timestamptz,
    CHECK ((user_id IS NULL) <> (service_account_id IS NULL))
);

CREATE INDEX auth_api_keys_user ON auth_api_keys (user_id, created_at) WHERE user_id IS NOT NULL;
CREATE INDEX auth_api_keys_service_account ON auth_api_keys (service_account_id, created_at) WHERE service_account_id IS NOT NULL;
CREATE INDEX auth_api_keys_expiry ON auth_api_keys (expires_at);
