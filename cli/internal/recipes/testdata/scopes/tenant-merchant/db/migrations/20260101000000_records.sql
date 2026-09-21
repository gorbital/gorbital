-- Records: each record belongs to one merchant, whose members reach
-- it through their role. orb gen module wrote this migration; change it
-- freely until it is released, then add a new one.

-- +goose Up
CREATE TABLE records (
    id          text        PRIMARY KEY,
    -- The merchant; NOT NULL, so row-level security covers the table.
    merchant_id text        NOT NULL,
    -- The member who created it, for display and audit. Access comes only
    -- from membership. Not a foreign key: records outlive their creator.
    created_by  text        NOT NULL,
    title       text        NOT NULL CHECK (char_length(title) BETWEEN 1 AND 100),
    note        text        NOT NULL DEFAULT '' CHECK (char_length(note) <= 2000),
    state       text        NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'done')),
    -- Increases with every update; an update must send the version it read.
    version     bigint      NOT NULL DEFAULT 1,
    created_at  timestamptz NOT NULL,
    updated_at  timestamptz NOT NULL,
    -- Other merchant tables reference (merchant_id, id), so a row can only
    -- point at a record of its own merchant.
    UNIQUE (merchant_id, id)
);

-- Title is unique per merchant, ignoring case.
CREATE UNIQUE INDEX records_merchant_title ON records (merchant_id, lower(title));

-- One index per sort of GET /v1/merchants/{merchantId}/records.
CREATE INDEX records_merchant_created ON records (merchant_id, created_at, id);
CREATE INDEX records_merchant_updated ON records (merchant_id, updated_at, id);
CREATE INDEX records_merchant_title_sort ON records (merchant_id, (lower(title) COLLATE "C"), id);

-- Purging a merchant should delete its records. gorbital doesn't
-- know which table holds your merchants, so add the foreign key
-- yourself:
--
--     ALTER TABLE records ADD CONSTRAINT records_merchant_id_fkey
--         FOREIGN KEY (merchant_id) REFERENCES <your merchant table> (id) ON DELETE CASCADE;

-- +goose Down
DROP TABLE records;
