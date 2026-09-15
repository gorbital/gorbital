-- Passkeys (ADR-0044): WebAuthn credentials for passwordless sign-in and as
-- a second factor, and the ceremonies waiting for a passkey's response.

-- +goose Up
-- The WebAuthn user handle stored in passkeys: 64 random bytes, set when the
-- first passkey is registered, never the user ID.
ALTER TABLE auth_users ADD COLUMN webauthn_user_handle bytea UNIQUE;

CREATE TABLE auth_passkeys (
    id              text        PRIMARY KEY,
    user_id         text        NOT NULL REFERENCES auth_users (id) ON DELETE CASCADE,
    credential_id   bytea       NOT NULL UNIQUE,
    -- The verified credential record from gorbital.dev/modules/auth/passkey:
    -- public key, flags, signature counter. Passed back to it unchanged.
    credential      jsonb       NOT NULL,
    name            text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    aaguid          bytea,
    backup_eligible boolean     NOT NULL,
    backup_state    boolean     NOT NULL,
    sign_count      bigint      NOT NULL DEFAULT 0,
    created_at      timestamptz NOT NULL,
    last_used_at    timestamptz
);

CREATE INDEX auth_passkeys_user ON auth_passkeys (user_id, created_at);

CREATE TABLE auth_webauthn_ceremonies (
    id               text        PRIMARY KEY,
    -- SHA-256 of the ceremony token; the token itself is never stored.
    token_hash       bytea       NOT NULL UNIQUE,
    -- NULL for a passwordless sign-in, which learns the account from the passkey.
    user_id          text        REFERENCES auth_users (id) ON DELETE CASCADE,
    -- reauth confirms a signed-in user's sensitive change, such as deleting
    -- the account.
    purpose          text        NOT NULL CHECK (purpose IN ('register', 'login', 'second_factor', 'reauth')),
    mfa_challenge_id text        REFERENCES auth_mfa_challenges (id) ON DELETE CASCADE,
    -- The challenge and options the response must match.
    session_data     jsonb       NOT NULL,
    expires_at       timestamptz NOT NULL,
    consumed_at      timestamptz,
    created_at       timestamptz NOT NULL
);

CREATE INDEX auth_webauthn_ceremonies_expiry ON auth_webauthn_ceremonies (expires_at);
