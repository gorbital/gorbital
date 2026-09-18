-- Orders: a customer orders from one restaurant. The order belongs to the
-- restaurant's organisation, but the customer who placed it and the courier
-- who carries it are members of no organisation at all — which is why three
-- different rules decide who may read one row (internal/modules/orders).
-- orb gen module --org wrote the table; the lines, the courier, the money and
-- the timestamps of the state machine were added here by hand.

-- +goose Up
-- docs:start orders-table
CREATE TABLE orders (
    id            text        PRIMARY KEY,
    -- The restaurant's organisation. Staff reach the order through it.
    org_id        text        NOT NULL,
    restaurant_id text        NOT NULL,
    -- The account that placed the order. Not a member of org_id, and not a
    -- foreign key: orders outlive accounts, and the customer's own rules
    -- live in the use cases.
    customer_id   text        NOT NULL,
    -- The courier carrying it, '' while none is assigned. Platform-scoped,
    -- like the couriers table: a courier delivers for many restaurants.
    courier_id    text        NOT NULL DEFAULT '',
    status        text        NOT NULL DEFAULT 'placed'
                              CHECK (status IN ('placed', 'accepted', 'preparing', 'ready',
                                                'collected', 'delivered', 'rejected', 'cancelled')),
    address       text        NOT NULL CHECK (char_length(address) BETWEEN 1 AND 200),
    note          text        NOT NULL DEFAULT '' CHECK (char_length(note) <= 500),
    -- Money in integer minor units (pence), never a float: 0.1 has no exact
    -- binary representation, and an order's total must equal the sum of its
    -- lines to the penny. The currency is stored with it so the number is
    -- never ambiguous.
    total_minor   bigint      NOT NULL CHECK (total_minor >= 0),
    currency      text        NOT NULL DEFAULT 'GBP' CHECK (char_length(currency) = 3),
    -- When the customer asked for it, NULL for "as soon as you can". Only
    -- accepted while the orders.scheduled_ordering flag is on, which is also
    -- what the customer app reads from GET /v1/flags to show the control.
    scheduled_for timestamptz,
    -- One timestamp per step it has actually taken; NULL until then. They
    -- are what the daily summary measures preparation time from.
    placed_at     timestamptz NOT NULL,
    accepted_at   timestamptz,
    ready_at      timestamptz,
    collected_at  timestamptz,
    delivered_at  timestamptz,
    -- When it reached a terminal state, whichever one: delivered, rejected
    -- or cancelled.
    closed_at     timestamptz,
    -- Why it was rejected or cancelled, for the customer and the audit log.
    closed_reason text        NOT NULL DEFAULT '' CHECK (char_length(closed_reason) <= 500),
    version       bigint      NOT NULL DEFAULT 1,
    created_at    timestamptz NOT NULL,
    updated_at    timestamptz NOT NULL,
    -- Other organisation tables reference (org_id, id), so a row can only
    -- point at an order of its own organisation.
    UNIQUE (org_id, id),
    FOREIGN KEY (org_id, restaurant_id) REFERENCES restaurants (org_id, id) ON DELETE CASCADE
);
-- docs:end orders-table

-- docs:start order-lines-table
-- The lines of an order. name and price_minor are a snapshot taken when the
-- order was placed, not a join to menu_items: a restaurant that renames a
-- dish or raises its price tomorrow must not rewrite what a customer ordered
-- and was charged today. item_id is kept for reporting only, and is not a
-- foreign key for the same reason — a deleted dish must not delete history.
CREATE TABLE order_lines (
    order_id    text     NOT NULL REFERENCES orders (id) ON DELETE CASCADE,
    position    smallint NOT NULL CHECK (position >= 0),
    item_id     text     NOT NULL,
    name        text     NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    price_minor bigint   NOT NULL CHECK (price_minor >= 0),
    quantity    smallint NOT NULL CHECK (quantity BETWEEN 1 AND 99),
    PRIMARY KEY (order_id, position)
);
-- docs:end order-lines-table

-- The restaurant's list: newest first, and by status.
CREATE INDEX orders_org_placed ON orders (org_id, placed_at DESC, id DESC);
CREATE INDEX orders_org_status ON orders (org_id, status, placed_at DESC, id DESC);
-- A customer's own orders, across every restaurant they have used.
CREATE INDEX orders_customer ON orders (customer_id, placed_at DESC, id DESC);
-- The courier's current work, and the restaurant's filter by courier.
CREATE INDEX orders_courier ON orders (courier_id, placed_at DESC, id DESC) WHERE courier_id <> '';
-- docs:start orders-late-index
-- The orders_late_sweep job's query: orders that are still being prepared.
-- A partial index, so the sweep visits only open orders however many
-- finished ones the table holds.
CREATE INDEX orders_open ON orders (accepted_at, org_id)
    WHERE status IN ('accepted', 'preparing', 'ready');
-- docs:end orders-late-index

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
DROP TABLE order_lines;
DROP TABLE orders;
