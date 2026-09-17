-- Idempotency keys (ADR-0060): one row per caller and key, holding the
-- response the key's request produced so a retry gets it again.

-- +goose Up
-- Logged, unlike ratelimit_buckets: losing rows in a crash would let a retry
-- run a request twice.
CREATE TABLE idempotency_keys (
    -- SHA-256 of the caller's scope and the key: neither user IDs nor
    -- client keys are stored.
    id           bytea       PRIMARY KEY,
    -- SHA-256 of the method, path, query and the body's SHA-256.
    fingerprint  bytea       NOT NULL,
    -- The random token of the request holding the key, and until when it
    -- holds it; both NULL once the response is stored.
    lock_token   bytea,
    locked_until timestamptz,
    -- When the key was claimed; rows expire idempotency.retention later.
    created_at   timestamptz NOT NULL,
    -- The stored response: NULL while the request is in progress. The body
    -- can hold personal data, so rows are deleted when they expire.
    status       smallint,
    header       jsonb,
    body         bytea
);

CREATE INDEX idempotency_keys_created_at ON idempotency_keys (created_at);
