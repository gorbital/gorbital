-- Records: every record is world-readable, so the table has no
-- ownership column and the reads are not filtered. orb gen module wrote this migration; change it
-- freely until it is released, then add a new one.

-- +goose Up
CREATE TABLE records (
    id         text        PRIMARY KEY,
    title      text        NOT NULL CHECK (char_length(title) BETWEEN 1 AND 100),
    note       text        NOT NULL DEFAULT '' CHECK (char_length(note) <= 2000),
    state      text        NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'done')),
    -- Increases with every update; an update must send the version it read.
    version    bigint      NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

-- Title is unique across every record, ignoring case.
CREATE UNIQUE INDEX records_title ON records (lower(title));

-- One index per sort of GET /v1/records.
CREATE INDEX records_created ON records (created_at, id);
CREATE INDEX records_updated ON records (updated_at, id);
CREATE INDEX records_title_sort ON records ((lower(title) COLLATE "C"), id);

-- +goose Down
DROP TABLE records;
