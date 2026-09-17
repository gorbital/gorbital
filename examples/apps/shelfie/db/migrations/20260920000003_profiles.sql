-- Readers' public profiles. A reader who registered with an email address
-- has one from the start (authhttp.RegisterFields); one who signed up with
-- Google, Apple or GitHub creates it with PUT /v1/profile.

-- +goose Up
CREATE TABLE profiles (
    -- The account's ID (auth_users.id).
    user_id      text        PRIMARY KEY,
    display_name text        NOT NULL CHECK (char_length(display_name) BETWEEN 1 AND 50),
    -- ISO 3166-1 alpha-2, or empty.
    country      text        NOT NULL DEFAULT '' CHECK (country ~ '^([A-Z]{2})?$'),
    -- Set by moderators; a suspended reader can't sign in.
    suspended_at timestamptz,
    created_at   timestamptz NOT NULL,
    updated_at   timestamptz NOT NULL
);
