-- Couriers: the people who carry the orders. A courier signs up to the
-- platform, not to a restaurant, and delivers for every restaurant that
-- picks them, so a courier belongs to no organisation and this table has no
-- org_id. It is the one table of this app outside the tenant pattern, and
-- the comment inside says why.

-- +goose Up
-- docs:start couriers-table
CREATE TABLE couriers (
    id         text        PRIMARY KEY,
    -- Where every other table of this app has "org_id text NOT NULL" there
    -- is nothing here, on purpose. A courier is a person who delivers for
    -- many restaurants and is employed by none of them, so there is no one
    -- organisation that owns the row and no tenant to scope it to. That has
    -- two consequences worth reading twice. This table is outside the
    -- row-level-security pattern the organisation tables use, because the
    -- policy those tables share ("the row's org_id is the organisation the
    -- connection is acting in") has nothing to compare here; and no route
    -- over this table can be guarded with guard.OrgMember, which only ever
    -- answers "is the caller staff of the organisation in the path". A
    -- courier's own routes are guarded with guard.Permission instead, and
    -- the ownership check (user_id below) is done in the use case.
    --
    -- The sign-in account behind the courier. One profile per account, which
    -- is what UNIQUE says, and what the use case turns into
    -- courier_already_registered.
    user_id    text        NOT NULL UNIQUE,
    -- The name the restaurant and the diner see. Never the user_id: the
    -- account behind a courier is not a restaurant's business.
    display_name text      NOT NULL CHECK (char_length(display_name) BETWEEN 1 AND 80),
    -- What they carry orders on; it decides how far a dispatcher will send
    -- them.
    vehicle    text        NOT NULL CHECK (vehicle IN ('bicycle', 'scooter', 'car', 'on_foot')),
    -- Whether they are working right now. Theirs to switch, not a
    -- restaurant's.
    available  boolean     NOT NULL DEFAULT false,
    -- The order they are carrying, empty when they are free. The orders
    -- module writes this column inside its own transaction when it assigns
    -- and releases a courier; the couriers module only reads it, to refuse a
    -- courier who tries to go off duty mid-delivery.
    active_order_id text   NOT NULL DEFAULT '',
    -- Increases with every update; an update must send the version it read.
    version    bigint      NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);
-- docs:end couriers-table

-- The dispatch list: the couriers a restaurant may pick right now, which is
-- the available ones that aren't already carrying something. The partial
-- index keeps the busy couriers out of it altogether, so the list stays
-- small however many couriers the platform has.
CREATE INDEX couriers_available ON couriers (available, id) WHERE active_order_id = '';

-- Deleting an account deletes its courier profile. auth_users is the
-- sign-in module's table (authhttp), which the app's migrations run with; an
-- app migrated without sign-in, such as another module's test app, still
-- gets this table, without the foreign key.
-- +goose StatementBegin
DO $$
BEGIN
    IF to_regclass('auth_users') IS NOT NULL THEN
        ALTER TABLE couriers ADD CONSTRAINT couriers_user_id_fkey
            FOREIGN KEY (user_id) REFERENCES auth_users (id) ON DELETE CASCADE;
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE couriers;
