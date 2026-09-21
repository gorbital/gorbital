-- Records: each record belongs to one organisation, whose members reach
-- it through their role. orb gen module wrote this migration; change it
-- freely until it is released, then add a new one.

-- +goose Up
CREATE TABLE records (
    id         text        PRIMARY KEY,
    -- The organisation; NOT NULL, so row-level security covers the table.
    org_id     text        NOT NULL,
    -- The member who created it, for display and audit. Access comes only
    -- from membership. Not a foreign key: records outlive their creator.
    created_by text        NOT NULL,
    title      text        NOT NULL CHECK (char_length(title) BETWEEN 1 AND 100),
    note       text        NOT NULL DEFAULT '' CHECK (char_length(note) <= 2000),
    state      text        NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'done')),
    -- Increases with every update; an update must send the version it read.
    version    bigint      NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    -- Other organisation tables reference (org_id, id), so a row can only
    -- point at a record of its own organisation.
    UNIQUE (org_id, id)
);

-- Title is unique per organisation, ignoring case.
CREATE UNIQUE INDEX records_org_title ON records (org_id, lower(title));

-- One index per sort of GET /v1/orgs/{orgId}/records.
CREATE INDEX records_org_created ON records (org_id, created_at, id);
CREATE INDEX records_org_updated ON records (org_id, updated_at, id);
CREATE INDEX records_org_title_sort ON records (org_id, (lower(title) COLLATE "C"), id);

-- Purging an organisation deletes its records. orgs is the organisations
-- module's table (orgshttp), which the app's migrations run with; an app
-- migrated without it, such as another module's test app, gets the table
-- without the foreign key.
-- +goose StatementBegin
DO $$
BEGIN
    IF to_regclass('orgs') IS NOT NULL THEN
        ALTER TABLE records ADD CONSTRAINT records_org_id_fkey
            FOREIGN KEY (org_id) REFERENCES orgs (id) ON DELETE CASCADE;
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE records;
