-- The trips travellers keep in the mobile app. Change this migration freely
-- until it is released; afterwards, add a new one.

-- docs:start table
-- +goose Up
CREATE TABLE trips (
    id          text        PRIMARY KEY,
    -- The traveller the trip belongs to: the actor's ID, which is the
    -- "sub" claim of the identity provider's token. There is no accounts
    -- table to reference: the provider owns the accounts, this app owns
    -- their trips.
    owner_id    text        NOT NULL CHECK (owner_id <> ''),
    destination text        NOT NULL CHECK (char_length(destination) BETWEEN 1 AND 120),
    notes       text        NOT NULL CHECK (char_length(notes) <= 2000),
    created_at  timestamptz NOT NULL
);

-- Every query filters by owner_id: the list reads it with created_at, and
-- the single-trip routes read it with the primary key.
CREATE INDEX trips_owner_created_at ON trips (owner_id, created_at DESC, id DESC);
-- docs:end table

-- +goose Down
DROP TABLE trips;
