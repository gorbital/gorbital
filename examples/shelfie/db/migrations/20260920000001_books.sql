-- Books on a reader's shelf. Change this migration freely until it is
-- released; afterwards, add a new one.

-- +goose Up
CREATE TABLE books (
    id         text        PRIMARY KEY,
    -- The signed-in user who added the book; only they can read or change it.
    owner_id   text        NOT NULL,
    title      text        NOT NULL CHECK (char_length(title) BETWEEN 1 AND 300),
    author     text        NOT NULL DEFAULT '' CHECK (char_length(author) <= 200),
    -- ISBN-10 or ISBN-13 without hyphens; NULL when unknown.
    isbn       text        CHECK (isbn ~ '^([0-9]{9}[0-9X]|[0-9]{13})$'),
    status     text        NOT NULL DEFAULT 'want_to_read' CHECK (status IN ('want_to_read', 'reading', 'read')),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

-- An ISBN appears once on each shelf.
CREATE UNIQUE INDEX books_owner_isbn ON books (owner_id, isbn) WHERE isbn IS NOT NULL;

-- GET /v1/books lists a shelf newest first.
CREATE INDEX books_owner_created ON books (owner_id, created_at DESC, id DESC);
