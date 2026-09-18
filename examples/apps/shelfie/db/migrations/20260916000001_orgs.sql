-- Organisations (ADR-0023, ADR-0048): organisations, their members with one
-- role each, and invitations. Change this migration freely until it is
-- released; afterwards, add a new one.

-- +goose Up
CREATE TABLE orgs (
    id          text        PRIMARY KEY,
    name        text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    -- A personal workspace: one owner, no invitations, deleted with the account.
    personal    boolean     NOT NULL DEFAULT false,
    -- The user who created it. Not a foreign key: the record outlives the account.
    created_by  text        NOT NULL,
    version     bigint      NOT NULL DEFAULT 1,
    created_at  timestamptz NOT NULL,
    updated_at  timestamptz NOT NULL,
    -- Set together when an owner deletes the organisation; the orgs_purge job
    -- removes it, with every org-scoped row, after purge_after.
    deleted_at  timestamptz,
    purge_after timestamptz,
    CHECK ((deleted_at IS NULL) = (purge_after IS NULL))
);

-- One personal workspace per user.
CREATE UNIQUE INDEX orgs_personal ON orgs (created_by) WHERE personal;
CREATE INDEX orgs_purge ON orgs (purge_after) WHERE deleted_at IS NOT NULL;

CREATE TABLE org_members (
    org_id    text        NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    -- Purging an account (after its retention) removes its memberships.
    user_id   text        NOT NULL REFERENCES auth_users (id) ON DELETE CASCADE,
    -- A role declared in the org catalog (internal/app/permissions.go).
    role      text        NOT NULL CHECK (role ~ '^[a-z][a-z0-9_]*$'),
    joined_at timestamptz NOT NULL,
    -- Who added the member: user:<id> or system:<name>.
    added_by  text        NOT NULL,
    PRIMARY KEY (org_id, user_id)
);

CREATE INDEX org_members_user ON org_members (user_id);

CREATE TABLE org_invitations (
    id               text        PRIMARY KEY,
    org_id           text        NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    email            text        NOT NULL,
    normalized_email text        NOT NULL,
    role             text        NOT NULL CHECK (role ~ '^[a-z][a-z0-9_]*$'),
    -- SHA-256 of the token in the invitation link; the token itself is never stored.
    token_hash       bytea       NOT NULL UNIQUE,
    invited_by       text        NOT NULL,
    created_at       timestamptz NOT NULL,
    -- When the email was last sent; resending counts toward the hourly limit.
    sent_at          timestamptz NOT NULL,
    expires_at       timestamptz NOT NULL,
    accepted_at      timestamptz,
    revoked_at       timestamptz
);

-- One open invitation per address per organisation.
CREATE UNIQUE INDEX org_invitations_open ON org_invitations (org_id, normalized_email)
    WHERE accepted_at IS NULL AND revoked_at IS NULL;
CREATE INDEX org_invitations_org_sent ON org_invitations (org_id, sent_at);
