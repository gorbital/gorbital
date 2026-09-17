-- Payments recorded from the provider's webhooks. Change this migration
-- freely until it is released; afterwards, add a new one.

-- docs:start table
-- +goose Up
CREATE TABLE payments (
    id                  text        PRIMARY KEY,
    -- The provider's delivery ID (webhook-id), which identifies the event,
    -- not the request: every retry of one event repeats it.
    event_id            text        NOT NULL CHECK (event_id <> ''),
    -- The payment the event is about, as the provider names it. Several
    -- rows share it: one per event, such as a payment and its refund.
    provider_payment_id text        NOT NULL CHECK (provider_payment_id <> ''),
    -- Minor units, always positive: 4999 is £49.99. status says which way
    -- the money moved.
    amount_minor        bigint      NOT NULL CHECK (amount_minor > 0),
    currency            char(3)     NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    status              text        NOT NULL CHECK (status IN ('succeeded', 'refunded')),
    -- When the provider says it happened, and when we recorded it.
    occurred_at         timestamptz NOT NULL,
    recorded_at         timestamptz NOT NULL
);

-- The idempotency of the webhook: a delivery already recorded can't be
-- recorded again, whoever sends it and however often (ON CONFLICT
-- (event_id) DO NOTHING in repository/insert_payment.go).
CREATE UNIQUE INDEX payments_event_id ON payments (event_id);

-- A payment's events, oldest first.
CREATE INDEX payments_provider_payment_id ON payments (provider_payment_id, occurred_at);
-- docs:end table

-- +goose Down
DROP TABLE payments;
