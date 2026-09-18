-- Orders: each order belongs to one organisation, whose members reach
-- it through their role. orb gen module wrote this migration; change it
-- freely until it is released, then add a new one.

-- +goose Up
CREATE TABLE orders (
    id         text        PRIMARY KEY,
    -- The organisation; NOT NULL, so row-level security covers the table.
    org_id     text        NOT NULL,
    -- The member who created it, for display and audit. Access comes only
    -- from membership. Not a foreign key: orders outlive their creator.
    created_by text        NOT NULL,
    status     text        NOT NULL DEFAULT 'placed' CHECK (status IN ('placed', 'accepted', 'preparing', 'ready', 'collected', 'delivered', 'rejected', 'cancelled')),
    address    text        NOT NULL CHECK (char_length(address) BETWEEN 1 AND 100),
    note       text        NOT NULL DEFAULT '' CHECK (char_length(note) <= 2000),
    -- Increases with every update; an update must send the version it read.
    version    bigint      NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    -- Other organisation tables reference (org_id, id), so a row can only
    -- point at an order of its own organisation.
    UNIQUE (org_id, id)
);

-- One index per sort of GET /v1/orgs/{orgId}/orders.
CREATE INDEX orders_org_created ON orders (org_id, created_at, id);
CREATE INDEX orders_org_updated ON orders (org_id, updated_at, id);
CREATE INDEX orders_org_address_sort ON orders (org_id, (lower(address) COLLATE "C"), id);

-- Purging an organisation deletes its orders. orgs is the organisations
-- module's table (orgshttp), which the app's migrations run with; an app
-- migrated without it, such as another module's test app, gets the table
-- without the foreign key.
-- +goose StatementBegin
DO $$
BEGIN
    IF to_regclass('orgs') IS NOT NULL THEN
        ALTER TABLE orders ADD CONSTRAINT orders_org_id_fkey
            FOREIGN KEY (org_id) REFERENCES orgs (id) ON DELETE CASCADE;
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE orders;
