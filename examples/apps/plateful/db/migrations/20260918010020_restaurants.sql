-- Restaurants: the tenant's own profile. Every restaurant is one
-- organisation, and every organisation has at most one restaurant, which is
-- what org_id UNIQUE says. orb gen module --org wrote the table; the opening
-- hours, the delivery radius and the suspension were added here by hand.

-- +goose Up
-- docs:start restaurants-table
CREATE TABLE restaurants (
    id         text        PRIMARY KEY,
    -- The organisation whose staff run this restaurant. One restaurant per
    -- organisation, so a member of an organisation is staff of exactly one.
    org_id     text        NOT NULL UNIQUE,
    -- The member who created the profile, for display and audit.
    created_by text        NOT NULL,
    name       text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    address    text        NOT NULL CHECK (char_length(address) BETWEEN 1 AND 200),
    cuisine    text        NOT NULL DEFAULT '' CHECK (char_length(cuisine) <= 60),
    -- Opening hours as minutes from midnight UTC, 0 to 1440. Two integers
    -- rather than a time range: they compare and index without a time zone
    -- each, and the apps render them in the diner's own.
    opens_minute  smallint NOT NULL DEFAULT 660  CHECK (opens_minute BETWEEN 0 AND 1440),
    closes_minute smallint NOT NULL DEFAULT 1320 CHECK (closes_minute BETWEEN 0 AND 1440),
    -- How far it delivers, in metres. The restaurants.max_delivery_radius_m
    -- setting caps what a restaurant may set.
    delivery_radius_m integer NOT NULL DEFAULT 3000 CHECK (delivery_radius_m > 0),
    -- onboarding until the restaurateur publishes it; open and paused are
    -- theirs to switch; suspended is platform staff's alone.
    status     text        NOT NULL DEFAULT 'onboarding'
                           CHECK (status IN ('onboarding', 'open', 'paused', 'suspended')),
    -- Why platform staff suspended it, shown to the restaurant. Empty
    -- unless the status is suspended.
    suspended_reason text  NOT NULL DEFAULT '' CHECK (char_length(suspended_reason) <= 500),
    -- The cover image customers see, or '' while there is none. The images
    -- module owns the row it names and the object behind it; this is a
    -- pointer, not a foreign key, because an image deleted there should
    -- leave the restaurant without a cover rather than refuse the deletion.
    cover_image_id text     NOT NULL DEFAULT '' CHECK (char_length(cover_image_id) <= 64),
    -- Increases with every update; an update must send the version it read.
    version    bigint      NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    -- Other organisation tables reference (org_id, id), so a row can only
    -- point at a restaurant of its own organisation.
    UNIQUE (org_id, id)
);
-- docs:end restaurants-table

-- Names are unique across the platform, ignoring case: two restaurants
-- called the same thing would confuse every customer who searches.
CREATE UNIQUE INDEX restaurants_name ON restaurants (lower(name));

-- The customers' list: open restaurants by name.
CREATE INDEX restaurants_status_name ON restaurants (status, (lower(name) COLLATE "C"), id);

-- Purging an organisation deletes its restaurant. orgs is the organisations
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
