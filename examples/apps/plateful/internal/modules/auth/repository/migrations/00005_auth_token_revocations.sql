-- Provider tokens waiting to be revoked (ADR-0046): queued in the transaction
-- that unlinks an identity or deletes an account, and revoked by the
-- auth_revoke_tokens job, with retries.

-- +goose Up
CREATE TABLE auth_token_revocations (
    id               text        PRIMARY KEY,
    provider         text        NOT NULL CHECK (provider IN ('google', 'apple')),
    -- The identity's subject, which the encryption is bound to; the identity
    -- itself is already gone.
    subject          text        NOT NULL,
    client_id        text        NOT NULL,
    -- The refresh token, still encrypted with AUTH_ENCRYPTION_KEYS.
    key_id           text        NOT NULL,
    token_ciphertext bytea       NOT NULL,
    attempts         integer     NOT NULL DEFAULT 0,
    -- When the next attempt is due; a claimed revocation is leased by moving
    -- it forward, so a crashed worker's revocations come back.
    next_attempt_at  timestamptz NOT NULL,
    last_error       text        NOT NULL DEFAULT '',
    created_at       timestamptz NOT NULL
);

CREATE INDEX auth_token_revocations_due ON auth_token_revocations (next_attempt_at);
