-- Restaurants: each restaurant belongs to one organisation, whose members reach
-- it through their role. orb gen module wrote this migration; change it
-- freely until it is released, then add a new one.

-- +goose Up
CREATE TABLE restaurants (
    id         text        PRIMARY KEY,
    -- The organisation; NOT NULL, so row-level security covers the table.
    org_id     text        NOT NULL,
    -- The member who created it, for display and audit. Access comes only
    -- from membership. Not a foreign key: restaurants outlive their creator.
    created_by text        NOT NULL,
    name       text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    address    text        NOT NULL CHECK (char_length(address) BETWEEN 1 AND 100),
    cuisine    text        NOT NULL CHECK (char_length(cuisine) BETWEEN 1 AND 100),
    status     text        NOT NULL DEFAULT 'onboarding' CHECK (status IN ('onboarding', 'open', 'paused', 'suspended')),
    -- Increases with every update; an update must send the version it read.
    version    bigint      NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    -- Other organisation tables reference (org_id, id), so a row can only
    -- point at a restaurant of its own organisation.
    UNIQUE (org_id, id)
);

-- Name is unique per organisation, ignoring case.
CREATE UNIQUE INDEX restaurants_org_name ON restaurants (org_id, lower(name));

-- One index per sort of GET /v1/orgs/{orgId}/restaurants.
CREATE INDEX restaurants_org_created ON restaurants (org_id, created_at, id);
CREATE INDEX restaurants_org_updated ON restaurants (org_id, updated_at, id);
CREATE INDEX restaurants_org_name_sort ON restaurants (org_id, (lower(name) COLLATE "C"), id);
CREATE INDEX restaurants_org_address_sort ON restaurants (org_id, (lower(address) COLLATE "C"), id);
CREATE INDEX restaurants_org_cuisine_sort ON restaurants (org_id, (lower(cuisine) COLLATE "C"), id);

-- Purging an organisation deletes its restaurants. orgs is the organisations
-- module's table (orgshttp), which the app's migrations run with; an app
-- migrated without it, such as another module's test app, gets the table
-- without the foreign key.
-- +goose StatementBegin
DO $$
BEGIN
    IF to_regclass('orgs') IS NOT NULL THEN
        ALTER TABLE restaurants ADD CONSTRAINT restaurants_org_id_fkey
            FOREIGN KEY (org_id) REFERENCES orgs (id) ON DELETE CASCADE;
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE restaurants;
