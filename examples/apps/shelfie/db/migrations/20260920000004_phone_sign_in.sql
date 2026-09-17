-- Phone-code sign-in (chapter 7): a reader confirms a phone number, then
-- signs in with a 6-digit code sent to it.

-- +goose Up
CREATE TABLE phone_numbers (
    -- The account's ID (auth_users.id); one number per account.
    user_id      text        PRIMARY KEY,
    -- E.164, such as +447700900123.
    phone        text        NOT NULL CHECK (phone ~ '^\+[1-9][0-9]{7,14}$'),
    -- NULL until the reader types the code sent to it.
    confirmed_at timestamptz,
    created_at   timestamptz NOT NULL
);

-- A confirmed number signs in to one account.
CREATE UNIQUE INDEX phone_numbers_confirmed ON phone_numbers (phone) WHERE confirmed_at IS NOT NULL;

CREATE TABLE phone_codes (
    id         text        PRIMARY KEY,
    user_id    text        NOT NULL,
    phone      text        NOT NULL,
    -- confirm: confirming a number; sign_in: signing in with it.
    purpose    text        NOT NULL CHECK (purpose IN ('confirm', 'sign_in')),
    -- SHA-256 of the code; the code itself is only in the text message.
    code_hash  bytea       NOT NULL,
    attempts   integer     NOT NULL DEFAULT 0,
    expires_at timestamptz NOT NULL,
    used_at    timestamptz,
    created_at timestamptz NOT NULL
);

CREATE INDEX phone_codes_phone ON phone_codes (phone, purpose, created_at DESC);
