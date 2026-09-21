-- Two-factor authentication (ADR-0043): authenticator app secrets encrypted
-- with AUTH_ENCRYPTION_KEYS, single-use recovery codes stored as hashes,
-- sign-ins waiting for a second factor, and whether a session was verified
-- with one.

-- +goose Up
CREATE TABLE auth_totp (
    user_id           text        PRIMARY KEY REFERENCES auth_users (id) ON DELETE CASCADE,
    -- ID of the AUTH_ENCRYPTION_KEYS key that encrypted the secret.
    key_id            text        NOT NULL,
    -- Nonce and AES-256-GCM ciphertext, bound to the user ID.
    secret_ciphertext bytea       NOT NULL,
    -- NULL until the user confirms a first code.
    confirmed_at      timestamptz,
    -- Newest time step used; a code is accepted only for a later step.
    last_used_step    bigint,
    created_at        timestamptz NOT NULL
);

CREATE TABLE auth_recovery_codes (
    id         text        PRIMARY KEY,
    user_id    text        NOT NULL REFERENCES auth_users (id) ON DELETE CASCADE,
    -- SHA-256 of the user ID and the normalized code.
    code_hash  bytea       NOT NULL,
    used_at    timestamptz,
    created_at timestamptz NOT NULL,
    UNIQUE (user_id, code_hash)
);

CREATE TABLE auth_mfa_challenges (
    id           text        PRIMARY KEY,
    user_id      text        NOT NULL REFERENCES auth_users (id) ON DELETE CASCADE,
    -- SHA-256 of the challenge token; the token itself is never stored.
    token_hash   bytea       NOT NULL UNIQUE,
    attempts     integer     NOT NULL DEFAULT 0,
    max_attempts integer     NOT NULL CHECK (max_attempts > 0),
    expires_at   timestamptz NOT NULL,
    consumed_at  timestamptz,
    ip           inet,
    user_agent   text        NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL
);

CREATE INDEX auth_mfa_challenges_expiry ON auth_mfa_challenges (expires_at);

-- Set when the session was created or confirmed with a second factor.
ALTER TABLE auth_sessions ADD COLUMN mfa_verified_at timestamptz;
