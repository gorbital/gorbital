-- Shared rate limits (ADR-0052): one row per limited key, decided with GCRA.

-- +goose Up
-- UNLOGGED: no write-ahead log, so decisions are cheap; a crash or failover
-- empties the table, which only resets rate limit budgets.
CREATE UNLOGGED TABLE ratelimit_buckets (
    -- SHA-256 of the limiter name and key: no email address or IP is stored.
    key bytea       PRIMARY KEY,
    -- The theoretical arrival time of the next request; once it has passed,
    -- the bucket is full again and the row can be deleted.
    tat timestamptz NOT NULL
);

CREATE INDEX ratelimit_buckets_tat ON ratelimit_buckets (tat);
