-- Records: each record belongs to one user. orb gen module wrote this
-- migration; change it freely until it is released, then add a new one.

-- +goose Up
CREATE TABLE records (
    id         text        PRIMARY KEY,
    -- The signed-in user who owns the record; only they can read or change it.
    owner_id   text        NOT NULL,
    title      text        NOT NULL CHECK (char_length(title) BETWEEN 1 AND 100),
    note       text        NOT NULL DEFAULT '' CHECK (char_length(note) <= 2000),
    state      text        NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'done')),
    -- Increases with every update; an update must send the version it read.
    version    bigint      NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

-- Title is unique per owner, ignoring case.
CREATE UNIQUE INDEX records_owner_title ON records (owner_id, lower(title));

-- One index per sort of GET /v1/records.
CREATE INDEX records_owner_created ON records (owner_id, created_at, id);
CREATE INDEX records_owner_updated ON records (owner_id, updated_at, id);
CREATE INDEX records_owner_title_sort ON records (owner_id, (lower(title) COLLATE "C"), id);

-- +goose Down
DROP TABLE records;
