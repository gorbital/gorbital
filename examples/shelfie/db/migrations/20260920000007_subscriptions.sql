-- Shelfie Plus, the paid plan that unlocks exporting a shelf (chapter 3).
-- One row per reader who has ever subscribed; the plan is active while
-- expires_at is in the future, so an expiry needs no nightly job.

-- +goose Up
-- docs:start subscriptions-table
CREATE TABLE subscriptions (
    -- The account (auth_users.id). One reader, one subscription.
    user_id    text        PRIMARY KEY,
    plan       text        NOT NULL CHECK (plan IN ('monthly', 'yearly')),
    -- When the paid period ends. A row in the past is a lapsed reader, kept
    -- so they can be offered the plan again.
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);
-- docs:end subscriptions-table
