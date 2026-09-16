-- Email suppression list (ADR-0062): addresses that bounced permanently or
-- complained, which the mail worker never sends to, and the provider
-- webhook deliveries already applied.

-- +goose Up
CREATE TABLE mail_suppressions (
    id         bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    -- Personal data: normalized (trimmed, lower case). Kept until an
    -- operator removes it; see the email guide.
    email      text        NOT NULL UNIQUE,
    reason     text        NOT NULL CHECK (reason IN ('bounce', 'complaint')),
    -- Who reported it, such as resend.
    source     text        NOT NULL,
    -- The provider's classification, such as Permanent/General; never the
    -- server's message, which can quote addresses.
    detail     text        NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT statement_timestamp(),
    -- The latest event for the address.
    updated_at timestamptz NOT NULL DEFAULT statement_timestamp()
);

-- Webhook deliveries applied recently, so a replayed request changes nothing.
CREATE TABLE mail_webhook_deliveries (
    -- The provider and its delivery ID, such as resend:msg_2mFz5wqS0b.
    key        text        PRIMARY KEY,
    expires_at timestamptz NOT NULL
);

CREATE INDEX mail_webhook_deliveries_expires_at ON mail_webhook_deliveries (expires_at);
