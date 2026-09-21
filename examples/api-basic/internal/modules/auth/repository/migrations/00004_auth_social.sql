-- Google and Apple sign-in (ADR-0046): provider identities linked to
-- accounts, web sign-ins waiting for the provider, and nonces for native apps.

-- +goose Up
CREATE TABLE auth_identities (
    id                       text        PRIMARY KEY,
    user_id                  text        NOT NULL REFERENCES auth_users (id) ON DELETE CASCADE,
    provider                 text        NOT NULL CHECK (provider IN ('google', 'apple')),
    -- The provider's stable ID of the person ("sub").
    subject                  text        NOT NULL,
    email                    text        NOT NULL DEFAULT '',
    private_email            boolean     NOT NULL DEFAULT false,
    name                     text        NOT NULL DEFAULT '',
    -- Apple's refresh token, encrypted with AUTH_ENCRYPTION_KEYS and kept only
    -- to revoke it, and the client ID it was issued for.
    refresh_key_id           text,
    refresh_token_ciphertext bytea,
    refresh_client_id        text,
    created_at               timestamptz NOT NULL,
    last_used_at             timestamptz,
    UNIQUE (provider, subject)
);

CREATE INDEX auth_identities_user ON auth_identities (user_id, created_at);

CREATE TABLE auth_oauth_states (
    id            text        PRIMARY KEY,
    -- SHA-256 of the state sent to the provider, and of the random value in
    -- the browser's __Host-oauth cookie; neither is stored.
    token_hash    bytea       NOT NULL UNIQUE,
    browser_hash  bytea       NOT NULL,
    provider      text        NOT NULL CHECK (provider IN ('google', 'apple')),
    nonce         text        NOT NULL,
    -- The PKCE verifier, useless without the authorization code.
    pkce_verifier text        NOT NULL,
    return_to     text        NOT NULL,
    expires_at    timestamptz NOT NULL,
    consumed_at   timestamptz,
    created_at    timestamptz NOT NULL
);

CREATE INDEX auth_oauth_states_expiry ON auth_oauth_states (expires_at);

CREATE TABLE auth_social_nonces (
    id          text        PRIMARY KEY,
    -- SHA-256 of the nonce given to a native app.
    token_hash  bytea       NOT NULL UNIQUE,
    provider    text        NOT NULL CHECK (provider IN ('google', 'apple')),
    expires_at  timestamptz NOT NULL,
    consumed_at timestamptz,
    created_at  timestamptz NOT NULL
);

CREATE INDEX auth_social_nonces_expiry ON auth_social_nonces (expires_at);
