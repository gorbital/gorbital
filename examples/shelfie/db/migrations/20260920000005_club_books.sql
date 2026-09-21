-- Club books: each club book belongs to one organisation, whose members reach
-- it through their role. orb gen module wrote this migration; change it
-- freely until it is released, then add a new one.

-- +goose Up
CREATE TABLE club_books (
    id         text        PRIMARY KEY,
    -- The organisation; NOT NULL, so row-level security covers the table.
    org_id     text        NOT NULL,
    -- The member who created it, for display and audit. Access comes only
    -- from membership. Not a foreign key: club books outlive their creator.
    created_by text        NOT NULL,
    title      text        NOT NULL CHECK (char_length(title) BETWEEN 1 AND 100),
    author     text        NOT NULL DEFAULT '' CHECK (char_length(author) <= 100),
    status     text        NOT NULL DEFAULT 'proposed' CHECK (status IN ('proposed', 'reading', 'finished')),
    note       text        NOT NULL DEFAULT '' CHECK (char_length(note) <= 2000),
    -- Increases with every update; an update must send the version it read.
    version    bigint      NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    -- Other organisation tables reference (org_id, id), so a row can only
    -- point at a club book of its own organisation.
    UNIQUE (org_id, id)
);

-- Title is unique per organisation, ignoring case.
CREATE UNIQUE INDEX club_books_org_title ON club_books (org_id, lower(title));

-- One index per sort of GET /v1/orgs/{orgId}/club-books.
CREATE INDEX club_books_org_created ON club_books (org_id, created_at, id);
CREATE INDEX club_books_org_updated ON club_books (org_id, updated_at, id);
CREATE INDEX club_books_org_title_sort ON club_books (org_id, (lower(title) COLLATE "C"), id);
CREATE INDEX club_books_org_author_sort ON club_books (org_id, (lower(author) COLLATE "C"), id);

-- Purging an organisation deletes its club books. orgs is the organisations
-- module's table (orgshttp), which the app's migrations run with; an app
-- migrated without it, such as another module's test app, gets the table
-- without the foreign key.
-- +goose StatementBegin
DO $$
BEGIN
    IF to_regclass('orgs') IS NOT NULL THEN
        ALTER TABLE club_books ADD CONSTRAINT club_books_org_id_fkey
            FOREIGN KEY (org_id) REFERENCES orgs (id) ON DELETE CASCADE;
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE club_books;
