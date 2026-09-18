-- A restaurant is an organisation. The organisation's name is the
-- restaurant's name, its id is the restaurant's id, and the fields below are
-- the rest of what customers see. They go on orgs, not on a side table,
-- because it is one concept: a second table with org_id UNIQUE would be one
-- row split in two, and every reader would have to learn which half holds
-- what.
--
-- The orgs table came from the organisations module
-- (20260916000001_orgs.sql), now the app's own code. This migration adds to
-- it rather than editing that file: a migration that has run anywhere is
-- history, and goose never runs it again, so a change made there would
-- reach new databases and silently miss every existing one.

-- +goose Up
-- docs:start restaurant-columns
ALTER TABLE orgs
    -- When the staff first saved the restaurant's profile; NULL until then.
    -- An organisation without one is not a restaurant anybody can see yet:
    -- a new sign-up's personal workspace, or a restaurateur still filling
    -- the form in.
    ADD COLUMN profile_created_at timestamptz,
    ADD COLUMN address text NOT NULL DEFAULT '' CHECK (char_length(address) <= 200),
    ADD COLUMN cuisine text NOT NULL DEFAULT '' CHECK (char_length(cuisine) <= 60),
    -- Opening hours as minutes from midnight UTC, 0 to 1440. Two integers
    -- rather than a time range: they compare and index without a time zone
    -- each, and the apps render them in the diner's own.
    ADD COLUMN opens_minute  smallint NOT NULL DEFAULT 660  CHECK (opens_minute BETWEEN 0 AND 1440),
    ADD COLUMN closes_minute smallint NOT NULL DEFAULT 1320 CHECK (closes_minute BETWEEN 0 AND 1440),
    -- How far it delivers, in metres. The restaurants.max_delivery_radius_m
    -- setting caps what a restaurant may set.
    ADD COLUMN delivery_radius_m integer NOT NULL DEFAULT 3000 CHECK (delivery_radius_m > 0),
    -- onboarding until the restaurateur publishes it; open and paused are
    -- theirs to switch; suspended is platform staff's alone.
    ADD COLUMN status text NOT NULL DEFAULT 'onboarding'
                           CHECK (status IN ('onboarding', 'open', 'paused', 'suspended')),
    -- Why platform staff suspended it, shown to the restaurant. Empty
    -- unless the status is suspended.
    ADD COLUMN suspended_reason text NOT NULL DEFAULT '' CHECK (char_length(suspended_reason) <= 500),
    -- The cover image customers see, or '' while there is none. The images
    -- module owns the row it names and the object behind it; this is a
    -- pointer, not a foreign key, because an image deleted there should
    -- leave the restaurant without a cover rather than refuse the deletion.
    ADD COLUMN cover_image_id text NOT NULL DEFAULT '' CHECK (char_length(cover_image_id) <= 64),
    -- Every organisation has the columns, but only a restaurant needs an
    -- address: the profile can't be saved without one.
    ADD CONSTRAINT orgs_restaurant_address
        CHECK (profile_created_at IS NULL OR char_length(address) >= 1);
-- docs:end restaurant-columns

-- Restaurant names are unique across the platform, ignoring case: two
-- restaurants called the same thing would confuse every customer who
-- searches. Partial, so organisations that aren't restaurants yet, such as
-- personal workspaces, may share a name.
CREATE UNIQUE INDEX orgs_restaurant_name ON orgs (lower(name)) WHERE profile_created_at IS NOT NULL;

-- The customers' list: open restaurants by name.
CREATE INDEX orgs_restaurant_status_name ON orgs (status, (lower(name) COLLATE "C"), id)
    WHERE profile_created_at IS NOT NULL;

-- +goose Down
DROP INDEX orgs_restaurant_status_name;
DROP INDEX orgs_restaurant_name;
ALTER TABLE orgs
    DROP CONSTRAINT orgs_restaurant_address,
    DROP COLUMN cover_image_id,
    DROP COLUMN suspended_reason,
    DROP COLUMN status,
    DROP COLUMN delivery_radius_m,
    DROP COLUMN closes_minute,
    DROP COLUMN opens_minute,
    DROP COLUMN cuisine,
    DROP COLUMN address,
    DROP COLUMN profile_created_at;
