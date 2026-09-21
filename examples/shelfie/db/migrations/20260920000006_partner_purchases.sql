-- Books a partner bookshop reports a reader bought, from the signed webhook
-- POST /v1/webhooks/partners/purchases (chapter 10).

-- +goose Up
-- docs:start partner-purchases
CREATE TABLE partner_purchases (
    id           text        PRIMARY KEY,
    -- The partner's own ID for the delivery. A sender retries, and a
    -- verified request can be replayed inside the signature's tolerance:
    -- this constraint is what makes recording a purchase idempotent.
    event_id     text        NOT NULL,
    partner      text        NOT NULL CHECK (char_length(partner) BETWEEN 1 AND 50),
    -- The account that bought it (auth_users.id).
    user_id      text        NOT NULL,
    isbn         text        NOT NULL CHECK (isbn ~ '^[0-9]{13}$'),
    title        text        NOT NULL CHECK (char_length(title) BETWEEN 1 AND 300),
    purchased_at timestamptz NOT NULL,
    created_at   timestamptz NOT NULL,
    UNIQUE (partner, event_id)
);

CREATE INDEX partner_purchases_by_reader ON partner_purchases (user_id, purchased_at DESC, id DESC);
-- docs:end partner-purchases
