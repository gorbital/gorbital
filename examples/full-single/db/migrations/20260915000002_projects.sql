-- Projects (ADR-0039): each project belongs to one user. Change this
-- migration freely until it is released; afterwards, add a new one.

-- +goose Up
CREATE TABLE projects (
    id          text        PRIMARY KEY,
    -- Purging an account (after its retention) deletes its projects.
    owner_id    text        NOT NULL REFERENCES auth_users (id) ON DELETE CASCADE,
    name        text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    description text        NOT NULL DEFAULT '' CHECK (char_length(description) <= 2000),
    status      text        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
    -- Increases with every update; an update must send the version it read.
    version     bigint      NOT NULL DEFAULT 1,
    created_at  timestamptz NOT NULL,
    updated_at  timestamptz NOT NULL
);

-- Name is unique per owner, ignoring case.
CREATE UNIQUE INDEX projects_owner_name ON projects (owner_id, lower(name));

-- One index per sort of GET /v1/projects.
CREATE INDEX projects_owner_created ON projects (owner_id, created_at, id);
CREATE INDEX projects_owner_updated ON projects (owner_id, updated_at, id);
CREATE INDEX projects_owner_name_sort ON projects (owner_id, (lower(name) COLLATE "C"), id);
