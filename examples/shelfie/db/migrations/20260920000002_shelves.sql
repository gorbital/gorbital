-- Shelves: each shelf belongs to one user. orb gen module wrote this
-- migration; change it freely until it is released, then add a new one.

-- +goose Up
CREATE TABLE shelves (
    id          text        PRIMARY KEY,
    -- The signed-in user who owns the shelf; only they can read or change it.
    owner_id    text        NOT NULL,
    name        text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    description text        NOT NULL DEFAULT '' CHECK (char_length(description) <= 2000),
    visibility  text        NOT NULL DEFAULT 'private' CHECK (visibility IN ('private', 'shared')),
    -- Increases with every update; an update must send the version it read.
    version     bigint      NOT NULL DEFAULT 1,
    created_at  timestamptz NOT NULL,
    updated_at  timestamptz NOT NULL
);

-- Name is unique per owner, ignoring case.
CREATE UNIQUE INDEX shelves_owner_name ON shelves (owner_id, lower(name));

-- One index per sort of GET /v1/shelves.
CREATE INDEX shelves_owner_created ON shelves (owner_id, created_at, id);
CREATE INDEX shelves_owner_updated ON shelves (owner_id, updated_at, id);
CREATE INDEX shelves_owner_name_sort ON shelves (owner_id, (lower(name) COLLATE "C"), id);

-- +goose Down
DROP TABLE shelves;
