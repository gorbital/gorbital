-- Announcements staff publish for customers. Change this migration freely
-- until it is released; afterwards, add a new one.

-- docs:start table
-- +goose Up
CREATE TABLE announcements (
    id           text        PRIMARY KEY,
    title        text        NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
    body         text        NOT NULL CHECK (char_length(body) BETWEEN 1 AND 5000),
    -- The staff member who published it: an actor ID, such as a user's.
    published_by text        NOT NULL CHECK (published_by <> ''),
    -- Shown from starts_at until ends_at.
    starts_at    timestamptz NOT NULL,
    ends_at      timestamptz NOT NULL,
    created_at   timestamptz NOT NULL,
    CHECK (ends_at > starts_at)
);

-- GET /v1/announcements lists the active ones, the retention job deletes
-- the expired ones: both by ends_at.
CREATE INDEX announcements_ends_at ON announcements (ends_at);
-- docs:end table

-- +goose Down
DROP TABLE announcements;
