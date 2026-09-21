-- Bans (ADR-0070): an operator can ban an account, which then can't sign
-- in; its sessions and API keys are revoked when the ban is set. Change
-- this migration freely until it is released; afterwards, add a new one.

-- +goose Up
ALTER TABLE auth_users
    ADD COLUMN banned_at     timestamptz,
    ADD COLUMN banned_reason text NOT NULL DEFAULT '';
